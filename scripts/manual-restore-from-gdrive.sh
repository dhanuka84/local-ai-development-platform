#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
compose_file=${COMPOSE_FILE:-$project_root/deploy/compose/compose.yaml}
compose_env=${COMPOSE_ENV:-$project_root/.env}
compose_project=${COMPOSE_PROJECT:-hybrid-ai-platform}
vault_runtime_dir=${VAULT_RUNTIME_DIR:-/dev/shm/hybrid-ai-platform-vault-$(id -u)}
archive_image=${BACKUP_ARCHIVE_IMAGE:-alpine:3.24.0}
restore_work_root=${RESTORE_WORK_ROOT:-${XDG_STATE_HOME:-${HOME:?HOME is required}/.local/state}/hybrid-ai-platform/restore}
stamp=${STAMP:?Set STAMP to the exact backup folder name}
confirmation=${CONFIRM_RESTORE:-}
allow_version_mismatch=${ALLOW_VERSION_MISMATCH:-false}
export VAULT_RUNTIME_DIR=$vault_runtime_dir

log() { printf '%s\n' "$*"; }
fail() { printf 'restore: %s\n' "$*" >&2; exit 1; }

for command_name in docker git jq sha256sum stat; do
    command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
if test -n "${GDRIVE_CLIENT_BIN:-}"; then
    test -x "$GDRIVE_CLIENT_BIN" || fail "Google Drive client is not executable: $GDRIVE_CLIENT_BIN"
    gdrive_command=("$GDRIVE_CLIENT_BIN")
else
    gdrive_python=${GDRIVE_PYTHON_BIN:-python3}
    command -v "$gdrive_python" >/dev/null 2>&1 || fail "Google Drive Python interpreter is unavailable: $gdrive_python"
    gdrive_command=("$gdrive_python" "$project_root/scripts/gdrive_client.py")
fi
case "$stamp" in
    *[!A-Za-z0-9._-]*|'') fail "STAMP contains unsafe characters" ;;
esac
test "$confirmation" = "$stamp" || fail "destructive restore requires CONFIRM_RESTORE=$stamp"
case "$allow_version_mismatch" in
    true|false) ;;
    *) fail "ALLOW_VERSION_MISMATCH must be true or false" ;;
esac
test -f "$compose_file" || fail "Compose file does not exist: $compose_file"
test -f "$compose_env" || fail "Compose environment file does not exist: $compose_env"

docker info >/dev/null 2>&1 || fail "cannot connect to the Docker daemon"
docker image inspect "$archive_image" >/dev/null 2>&1 || fail \
    "archive image is not local; pull $archive_image before starting a restore"
archive_image_id=$(docker image inspect --format '{{.Id}}' "$archive_image")

compose=(docker compose -p "$compose_project" --env-file "$compose_env" -f "$compose_file")
compose_json=$("${compose[@]}" config --format json) || fail "Compose configuration is invalid"
test "$(jq -r '.name' <<<"$compose_json")" = "$compose_project" || fail "Compose project name mismatch"
service_images=$("${compose[@]}" images --format json | jq -sc \
    'if length == 1 and (.[0] | type) == "array" then .[0] else . end
     | map({container:.ContainerName,repository:.Repository,tag:.Tag,id:.ID}) | sort_by(.container)')

if test -e "$restore_work_root"; then
    test -d "$restore_work_root" && test ! -L "$restore_work_root" && test -O "$restore_work_root" || \
        fail "RESTORE_WORK_ROOT must be an owned, non-symlink directory"
    work_mode=$(stat -c '%a' "$restore_work_root")
    test "$((8#$work_mode & 077))" -eq 0 || fail "RESTORE_WORK_ROOT permissions must be 0700 or stricter"
else
    mkdir -p "$restore_work_root"
    chmod 700 "$restore_work_root"
fi
work_dir=$(mktemp -d "$restore_work_root/${stamp}.XXXXXX")
control_dir=$work_dir/bundle
declare -a staging_volumes=()
restore_started=false
platform_stopped=false
declare -a running_services=()

cleanup() {
    local original_status=$?
    trap - EXIT
    for staging_volume in "${staging_volumes[@]}"; do
        docker volume rm "$staging_volume" >/dev/null 2>&1 || true
    done
    if test -d "$control_dir" && test ! -L "$control_dir"; then
        for control_file in "$control_dir"/* "$control_dir"/.[!.]*; do
            test -e "$control_file" || continue
            if test -f "$control_file" && test ! -L "$control_file"; then
                unlink "$control_file"
            fi
        done
        rmdir "$control_dir" 2>/dev/null || true
    fi
    rmdir "$work_dir" 2>/dev/null || true
    if ((original_status != 0)) && test "$restore_started" = true; then
        "${compose[@]}" stop >/dev/null 2>&1 || true
        printf '%s\n' "restore: failed after volume replacement began; the platform was intentionally left stopped" >&2
    elif ((original_status != 0)) && test "$platform_stopped" = true && ((${#running_services[@]} > 0)); then
        printf '%s\n' "restore: validation failed after stopping services; restarting the original service set" >&2
        "${compose[@]}" start "${running_services[@]}" >/dev/null 2>&1 || \
            printf '%s\n' "restore: could not restart every original service" >&2
    fi
    exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

log "Downloading, authenticating, and decrypting the backup bundle..."
download_receipt=$("${gdrive_command[@]}" download-bundle \
    --stamp "$stamp" --output-dir "$control_dir")
jq -e \
    --arg stamp "$stamp" \
    'select(.schema == "hybrid-ai/gdrive-download-receipt/v1" and .stamp == $stamp)
     | .files | select(type == "array")' \
    <<<"$download_receipt" >/dev/null || fail "Google Drive client returned an invalid download receipt"
jq -e \
    --arg stamp "$stamp" \
    --arg project "$compose_project" \
    '.schema == "hybrid-ai/cold-volume-backup/v1" and .backup_type == "cold-docker-volume"
     and .stamp == $stamp and .compose.project == $project and (.volumes | type == "array")' \
    "$control_dir/manifest.json" >/dev/null || fail "manifest validation failed"

expected_manifest_hash=$(awk '$2 == "manifest.json" {print $1}' "$control_dir/SHA256SUMS.txt")
actual_manifest_hash=$(sha256sum "$control_dir/manifest.json" | cut -d' ' -f1)
test -n "$expected_manifest_hash" && test "$actual_manifest_hash" = "$expected_manifest_hash" || \
    fail "manifest checksum validation failed"

current_revision=$(git -C "$project_root" rev-parse HEAD)
current_compose_sha256=$(sha256sum "$compose_file" | cut -d' ' -f1)
backup_revision=$(jq -r '.repository.revision' "$control_dir/manifest.json")
backup_compose_sha256=$(jq -r '.compose.file_sha256' "$control_dir/manifest.json")
backup_archive_image_id=$(jq -r '.archive_image.id' "$control_dir/manifest.json")
backup_service_images=$(jq -cS '.service_images' "$control_dir/manifest.json")
if test "$allow_version_mismatch" != true; then
    test "$current_revision" = "$backup_revision" || fail \
        "repository revision differs from the backup; inspect compatibility, then set ALLOW_VERSION_MISMATCH=true"
    test "$current_compose_sha256" = "$backup_compose_sha256" || fail \
        "Compose file differs from the backup; inspect compatibility, then set ALLOW_VERSION_MISMATCH=true"
    test "$archive_image_id" = "$backup_archive_image_id" || fail \
        "archive utility image differs from the backup; inspect compatibility, then set ALLOW_VERSION_MISMATCH=true"
    test "$(jq -cS . <<<"$service_images")" = "$backup_service_images" || fail \
        "service image IDs differ from the backup; inspect compatibility, then set ALLOW_VERSION_MISMATCH=true"
fi

resolve_volume() {
    local logical_name=$1
    local actual_name
    actual_name=$(jq -r --arg name "$logical_name" '.volumes[$name].name // empty' <<<"$compose_json")
    test -n "$actual_name" || fail "Compose volume is not defined: $logical_name"
    printf '%s\n' "$actual_name"
}

required_volumes=(postgres-data artifact-data etcd-data minio-data milvus-data)
optional_volumes=(cerbos-audit ollama-data analyzer-cache)
restore_volumes=()
for logical_name in "${required_volumes[@]}"; do
    jq -e --arg name "$logical_name" \
        '.volumes | any(.logical_name == $name and .archive == ($name + ".tar.gz"))' \
        "$control_dir/manifest.json" >/dev/null || fail "manifest is missing required volume $logical_name"
    restore_volumes+=("$logical_name")
done
for logical_name in "${optional_volumes[@]}"; do
    if jq -e --arg name "$logical_name" \
        '.volumes | any(.logical_name == $name and .archive == ($name + ".tar.gz"))' \
        "$control_dir/manifest.json" >/dev/null; then
        restore_volumes+=("$logical_name")
    fi
done

declare -A destination_volumes
declare -A staged_by_logical_name
for logical_name in "${restore_volumes[@]}"; do
    destination_volumes[$logical_name]=$(resolve_volume "$logical_name")
    archive_name=$logical_name.tar.gz
    expected_hash=$(awk -v archive="$archive_name" '$2 == archive {print $1}' "$control_dir/SHA256SUMS.txt")
    test -n "$expected_hash" || fail "checksum list is missing $archive_name"
    staging_volume="${compose_project}_restore-${stamp}-${logical_name}-$$"
    docker volume create "$staging_volume" >/dev/null
    staging_volumes+=("$staging_volume")
    staged_by_logical_name[$logical_name]=$staging_volume

    actual_hash=$(sha256sum "$control_dir/$archive_name" | cut -d' ' -f1)
    test "$actual_hash" = "$expected_hash" || fail "checksum validation failed for $archive_name"
    log "Staging authenticated archive $logical_name..."
    docker run --rm --interactive --network none --volume "$staging_volume:/volume" "$archive_image" \
        tar xzpf - -C /volume <"$control_dir/$archive_name" || fail "failed to stage $archive_name"
done

mapfile -t running_services < <("${compose[@]}" ps --services --status running)
if ((${#running_services[@]} > 0)); then
    log "Stopping platform services before volume replacement..."
    platform_stopped=true
    "${compose[@]}" stop
fi
for logical_name in "${restore_volumes[@]}"; do
    destination_volume=${destination_volumes[$logical_name]}
    if docker ps --quiet --filter "volume=$destination_volume" | grep -q .; then
        fail "destination volume is still attached to a running container: $destination_volume"
    fi
done

restore_started=true
for logical_name in "${restore_volumes[@]}"; do
    staging_volume=${staged_by_logical_name[$logical_name]}
    destination_volume=${destination_volumes[$logical_name]}
    docker volume create "$destination_volume" >/dev/null
    log "Replacing $logical_name..."
    docker run --rm --network none --volume "$staging_volume:/source:ro" "$archive_image" \
        tar cpf - -C /source . \
        | docker run --rm --interactive --network none --volume "$destination_volume:/destination" "$archive_image" \
            sh -euc 'find /destination -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +; tar xpf - -C /destination'
done

log "Starting the restored platform..."
"${compose[@]}" up -d
restore_started=false
platform_stopped=false
log "Restore completed. Run: make mcp-status && make doctor && make repository-org-queue-status"
