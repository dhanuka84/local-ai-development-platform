#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
compose_file=${COMPOSE_FILE:-$project_root/deploy/compose/compose.yaml}
compose_env=${COMPOSE_ENV:-$project_root/.env}
compose_project=${COMPOSE_PROJECT:-hybrid-ai-platform}
vault_runtime_dir=${VAULT_RUNTIME_DIR:-/dev/shm/hybrid-ai-platform-vault-$(id -u)}
archive_image=${BACKUP_ARCHIVE_IMAGE:-alpine:3.24.0}
backup_root=${BACKUP_ROOT:-${XDG_STATE_HOME:-${HOME:?HOME is required}/.local/state}/hybrid-ai-platform/backups}
classification=${BACKUP_DATA_CLASSIFICATION:-}
stamp=${STAMP:-$(date -u +%Y%m%d-%H%M%SZ)}
include_ollama=${INCLUDE_OLLAMA:-false}
include_analyzer_cache=${INCLUDE_ANALYZER_CACHE:-false}
backup_dir=$backup_root/cold/$stamp
export VAULT_RUNTIME_DIR=$vault_runtime_dir

log() { printf '%s\n' "$*"; }
fail() { printf 'backup: %s\n' "$*" >&2; exit 1; }

for command_name in docker git jq sha256sum; do
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
for value_name in include_ollama include_analyzer_cache; do
    value=${!value_name}
    case "$value" in
        true|false) ;;
        *) fail "${value_name^^} must be true or false" ;;
    esac
done
case "$stamp" in
    *[!A-Za-z0-9._-]*|'') fail "STAMP contains unsafe characters" ;;
esac
test "$classification" = approved-for-encrypted-cloud || fail \
    "set BACKUP_DATA_CLASSIFICATION=approved-for-encrypted-cloud after confirming this local data is allowed in encrypted Google Drive storage"
test -f "$compose_file" || fail "Compose file does not exist: $compose_file"
test -f "$compose_env" || fail "Compose environment file does not exist: $compose_env"
test ! -e "$backup_dir" || fail "backup directory already exists: $backup_dir"

docker info >/dev/null 2>&1 || fail "cannot connect to the Docker daemon"
docker image inspect "$archive_image" >/dev/null 2>&1 || fail \
    "archive image is not local; pull $archive_image before starting a cold backup"
archive_image_id=$(docker image inspect --format '{{.Id}}' "$archive_image")

compose=(docker compose -p "$compose_project" --env-file "$compose_env" -f "$compose_file")
compose_json=$("${compose[@]}" config --format json) || fail "Compose configuration is invalid"
test "$(jq -r '.name' <<<"$compose_json")" = "$compose_project" || fail "Compose project name mismatch"
service_images=$("${compose[@]}" images --format json | jq -sc \
    'if length == 1 and (.[0] | type) == "array" then .[0] else . end
     | map({container:.ContainerName,repository:.Repository,tag:.Tag,id:.ID}) | sort_by(.container)')

resolve_volume() {
    local logical_name=$1
    local actual_name
    actual_name=$(jq -r --arg name "$logical_name" '.volumes[$name].name // empty' <<<"$compose_json")
    test -n "$actual_name" || fail "Compose volume is not defined: $logical_name"
    docker volume inspect "$actual_name" >/dev/null 2>&1 || fail "Docker volume does not exist: $actual_name"
    printf '%s\n' "$actual_name"
}

logical_volumes=(postgres-data artifact-data etcd-data minio-data milvus-data)
if docker volume inspect "$(jq -r '.volumes["cerbos-audit"].name // empty' <<<"$compose_json")" >/dev/null 2>&1; then
    logical_volumes+=(cerbos-audit)
fi
if test "$include_ollama" = true; then
    logical_volumes+=(ollama-data)
fi
if test "$include_analyzer_cache" = true; then
    logical_volumes+=(analyzer-cache)
fi

declare -A actual_volumes
for logical_name in "${logical_volumes[@]}"; do
    actual_volumes[$logical_name]=$(resolve_volume "$logical_name")
done

mkdir -p "$backup_dir"
mapfile -t running_services < <("${compose[@]}" ps --services --status running)
platform_stopped=false

restart_platform() {
    local original_status=$?
    local restart_status=0
    trap - EXIT
    if test "$platform_stopped" = true && ((${#running_services[@]} > 0)); then
        log "Restarting services that were running before the backup..."
        "${compose[@]}" start "${running_services[@]}" || restart_status=$?
    fi
    if ((original_status != 0)); then
        exit "$original_status"
    fi
    exit "$restart_status"
}
trap restart_platform EXIT
trap 'exit 130' INT TERM

if ((${#running_services[@]} > 0)); then
    log "Stopping platform services for a consistent snapshot..."
    platform_stopped=true
    "${compose[@]}" stop
fi

for logical_name in "${logical_volumes[@]}"; do
    actual_name=${actual_volumes[$logical_name]}
    if docker ps --quiet --filter "volume=$actual_name" | grep -q .; then
        fail "volume is still attached to a running container: $actual_name"
    fi
done

volume_manifest='[]'
for logical_name in "${logical_volumes[@]}"; do
    actual_name=${actual_volumes[$logical_name]}
    archive_name=$logical_name.tar.gz
    temporary_archive=$backup_dir/.$archive_name.partial
    log "Archiving $logical_name..."
    docker run --rm --network none --volume "$actual_name:/volume:ro" "$archive_image" \
        tar czf - -C /volume . >"$temporary_archive"
    test -s "$temporary_archive" || fail "archive is empty: $archive_name"
    mv "$temporary_archive" "$backup_dir/$archive_name"
    volume_manifest=$(jq -cn \
        --argjson current "$volume_manifest" \
        --arg logical_name "$logical_name" \
        --arg source_volume "$actual_name" \
        --arg archive "$archive_name" \
        '$current + [{logical_name:$logical_name,source_volume:$source_volume,archive:$archive}]')
done

repository_revision=$(git -C "$project_root" rev-parse HEAD)
if test -n "$(git -C "$project_root" status --porcelain=v1 --untracked-files=normal)"; then
    repository_dirty=true
else
    repository_dirty=false
fi
compose_file_sha256=$(sha256sum "$compose_file" | cut -d' ' -f1)
compose_config_sha256=$(printf '%s' "$compose_json" | sha256sum | cut -d' ' -f1)
jq -n \
    --arg schema hybrid-ai/cold-volume-backup/v1 \
    --arg backup_type cold-docker-volume \
    --arg created_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg stamp "$stamp" \
    --arg compose_project "$compose_project" \
    --arg compose_file_sha256 "$compose_file_sha256" \
    --arg compose_config_sha256 "$compose_config_sha256" \
    --arg repository_revision "$repository_revision" \
    --arg archive_image_reference "$archive_image" \
    --arg archive_image_id "$archive_image_id" \
    --argjson repository_dirty "$repository_dirty" \
    --argjson service_images "$service_images" \
    --argjson volumes "$volume_manifest" \
    '{schema:$schema,backup_type:$backup_type,created_at:$created_at,stamp:$stamp,
      compose:{project:$compose_project,file_sha256:$compose_file_sha256,config_sha256:$compose_config_sha256},
      repository:{revision:$repository_revision,dirty:$repository_dirty},
      archive_image:{reference:$archive_image_reference,id:$archive_image_id},
      service_images:$service_images,volumes:$volumes}' \
    >"$backup_dir/manifest.json"

(
    cd "$backup_dir"
    sha256sum -- ./*.tar.gz ./manifest.json | sed 's|  \./|  |' | sort -k2 >SHA256SUMS.txt
)

if ((${#running_services[@]} > 0)); then
    log "Restarting services that were running before the backup..."
    "${compose[@]}" start "${running_services[@]}"
    platform_stopped=false
fi

log "Encrypting locally and uploading with the Google Drive API..."
upload_receipt=$("${gdrive_command[@]}" upload-bundle \
    --stamp "$stamp" --source-dir "$backup_dir")
jq -e \
    --arg stamp "$stamp" \
    'select(.schema == "hybrid-ai/gdrive-upload-receipt/v1" and .stamp == $stamp)
     | .files | select(type == "array")' \
    <<<"$upload_receipt" >/dev/null || fail "Google Drive client returned an invalid upload receipt"

trap - EXIT
log "Cold backup completed: $backup_dir"
log "Encrypted Google Drive upload completed for backup: $stamp"
