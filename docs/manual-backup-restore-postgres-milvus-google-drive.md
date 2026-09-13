# Manual backup and restore: PostgreSQL, Milvus, and Google Drive

Documentation reviewed September 12, 2026. See the [operations runbook](operations.md)
for current credential handling and the [developer guide](developer-guide.md)
for disposable backup tests. Production retention and recovery targets remain
[deferred acceptance](agent-ready-operating-decisions.md#retention-and-enterprise-deployment).

This runbook covers operator-triggered cold backups of the platform's durable
Docker volumes. The implementation uses the Google Drive API directly; rclone
is not required.

The entry points are:

- `scripts/manual-backup-to-gdrive.sh`
- `scripts/manual-restore-from-gdrive.sh`
- `scripts/gdrive_client.py`
- `make gdrive-*`, `make backup-gdrive`, and `make restore-gdrive`

## Security model

The Google account password is never requested, stored, or handled by these
tools. Browser-based OAuth obtains a refresh token using the narrow
`drive.file` scope.

The encrypted local vault holds:

- `GDRIVE_CLIENT_ID`
- `GDRIVE_CLIENT_SECRET`
- `GDRIVE_REFRESH_TOKEN`
- `GDRIVE_FOLDER_ID`
- `BACKUP_ENCRYPTION_KEY`

`GDRIVE_FOLDER_ID` may contain either the folder ID or the complete Google Drive
folder URL. The client reads all Google values directly into the Python process
after vault authentication; it does not materialize them in `/dev/shm`, place
them on a command line, or include Drive identifiers and links in command
receipts.

Every bundle object is encrypted locally with an independent random nonce and
AES-256-GCM before upload. The envelope authenticates both ciphertext and a
versioned context. Google receives only `.enc` objects. After every resumable
upload, the client compares its ciphertext SHA-256 with the checksum reported
by Drive. A backup folder is marked `complete` only after every object passes.
On upload failure, the incomplete app-created folder is moved to Drive trash.

Local cold-backup archives under `BACKUP_ROOT` remain plaintext so they can be
used for local disaster recovery. They are created with a `0700` directory and
`0600` files. Protect that filesystem with full-disk encryption and retention
controls. Restore downloads are decrypted in an owned `0700` work directory
and removed after staging or failure.

## One-time Google setup

1. In a Google Cloud project, enable the Google Drive API.
2. Configure the OAuth consent screen. For an app in testing, add the Google
   account used for backups as a test user.
3. Create an OAuth 2.0 Client ID with application type **Desktop app**.
4. If a client secret was pasted into a terminal, screenshot, chat, issue, or
   log, delete that OAuth client and create a new one before continuing.
5. Install the isolated Python dependencies:

   ```sh
   make gdrive-install
   ```

6. Store the destination folder and new OAuth values in the existing vault:

   ```sh
   make vault-set NAME=GDRIVE_FOLDER_ID
   make vault-set NAME=GDRIVE_CLIENT_ID
   make vault-set NAME=GDRIVE_CLIENT_SECRET
   ```

   At the hidden `Value for GDRIVE_FOLDER_ID:` prompt, paste either the complete
   folder URL or only its ID. Enter the local vault passphrase at each
   `Vault passphrase:` prompt and enter each requested value only at its hidden
   `Value for ...:` prompt.

   If an older checkout already has `GDRIVE_FOLDER_ID` in `.env`, migrate and
   scrub that line atomically instead of copying it through a visible command:

   ```sh
   make vault-import-env NAME=GDRIVE_FOLDER_ID
   ```

   The client uses this existing folder as the parent for app-created backup
   folders. `gdrive-check` creates and immediately trashes a zero-byte marker
   to verify `drive.file` write access.

7. New vaults created by `make env-init` already contain an independent backup
   key. For an older vault that does not, generate it once:

   ```sh
   make vault-set NAME=BACKUP_ENCRYPTION_KEY GENERATE=true
   ```

8. Authorize in the local browser:

   ```sh
   make gdrive-auth
   ```

   Enter the **vault passphrase** in the terminal. Then sign in to Google in
   the browser and approve the Drive access request. The resulting refresh
   token is written back into the encrypted vault. Do not enter the Google
   account password at a terminal prompt.

9. Verify OAuth refresh and destination access:

   ```sh
   make gdrive-check
   make vault-list
   ```

`vault-list` shows names only. It must include the five Google/backup names
listed above. Vault ciphertext and recovery copies under `.local/` are ignored
by Git, but they are still sensitive operational files.

## Backup preconditions

- Docker is reachable.
- The pinned archive image is already present locally. Backup does not pull an
  image while the platform is stopped.
- The encrypted vault is readable and OAuth has been configured.
- The configured Drive folder is writable.
- The operator has explicitly classified the volume data as allowed in
  encrypted cloud storage.
- There is sufficient local space under `BACKUP_ROOT` for the archives and one
  additional largest-file encryption buffer during upload.

The default required volumes are:

- `postgres-data`
- `artifact-data`
- `etcd-data`
- `minio-data`
- `milvus-data`

`cerbos-audit` is included when present. `ollama-data` and `analyzer-cache` are
excluded by default because they are large or rebuildable; include them only
with `INCLUDE_OLLAMA=true` or `INCLUDE_ANALYZER_CACHE=true`.

## Run a backup

```sh
make backup-gdrive \
  BACKUP_DATA_CLASSIFICATION=approved-for-encrypted-cloud
```

To choose a reproducible stamp or a different private local root:

```sh
make backup-gdrive \
  STAMP=20260830-203000Z \
  BACKUP_ROOT=/encrypted/local/backups \
  BACKUP_DATA_CLASSIFICATION=approved-for-encrypted-cloud
```

The backup procedure:

1. Resolves the exact Compose volume names and records image/repository
   compatibility data.
2. Records exactly which Compose services are running.
3. Stops the platform and refuses to archive any volume still attached to a
   running container.
4. Creates gzip-compressed volume archives, `manifest.json`, and
   `SHA256SUMS.txt` in a private local directory.
5. Restarts exactly the services that were running before the snapshot.
6. Authenticates to the local vault, encrypts each bundle file, and performs
   resumable uploads through the Drive API.
7. Compares every local ciphertext SHA-256 to the Drive-reported SHA-256.
8. Marks the Drive folder complete and reports success without printing Drive
   folder IDs or URLs.

An existing backup stamp is never overwritten.

## Download and decrypt without restoring

To inspect or retain a decrypted bundle without changing Docker volumes:

```sh
make download-gdrive STAMP=20260830-203000Z
```

The default output is `.local/gdrive-downloads/<stamp>`. The target creates an
owned `0700` download root, refuses symlinks or an existing stamp directory,
downloads every `.enc` object, verifies the Drive ciphertext SHA-256,
authenticates AES-GCM, and validates the complete plaintext checksum manifest.
The decrypted files are mode `0600` and remain on disk until the operator
removes them.

Use an encrypted filesystem for a different destination:

```sh
make download-gdrive \
  STAMP=20260830-203000Z \
  DOWNLOAD_ROOT=/encrypted/private/gdrive-downloads
```

Do not regenerate `BACKUP_ENCRYPTION_KEY`: the key used at upload time is
required to decrypt the bundle.

## Restore preconditions

Restores are destructive. Before starting:

- Take a fresh backup of the current state.
- Confirm that the requested stamp is the intended recovery point.
- Ensure the archive utility image referenced by the backup is local.
- Ensure `RESTORE_WORK_ROOT` has enough space for the decrypted bundle and is
  on encrypted local storage. The default is
  `~/.local/state/hybrid-ai-platform/restore`.
- Keep the vault materialized for Compose startup. The Make target performs
  this check before restore.

By default, restore refuses a repository revision, Compose file, archive image,
or service-image mismatch. Inspect any mismatch before using the explicit
override.

## Run a restore

```sh
make restore-gdrive \
  STAMP=20260830-203000Z \
  CONFIRM_RESTORE=20260830-203000Z
```

After deliberate compatibility review only:

```sh
make restore-gdrive \
  STAMP=20260830-203000Z \
  CONFIRM_RESTORE=20260830-203000Z \
  ALLOW_VERSION_MISMATCH=true
```

The restore procedure:

1. Finds exactly one app-created backup folder marked `complete`.
2. Downloads each encrypted object and verifies the Drive ciphertext SHA-256.
3. Authenticates AES-GCM before making a final plaintext file visible.
4. Validates the manifest and all plaintext SHA-256 entries.
5. Extracts every archive into disposable staging volumes while the current
   platform remains untouched.
6. Stops the currently running service set only after all staging succeeds.
7. Replaces destination volume contents and starts the restored platform.
8. Removes staging volumes and the decrypted restore work directory.

If validation or staging fails, no live volume is changed. If failure occurs
after replacement begins, the platform is intentionally left stopped for
operator intervention.

## Validation and troubleshooting

Run local cryptographic and bundle tests:

```sh
make vault-test
make gdrive-test
```

Run the disposable Docker integration test when Docker and the archive image
are available:

```sh
make backup-restore-test
```

Common failures:

- `vault authentication failed`: the local vault passphrase is wrong or the
  encrypted file changed. Use the documented runtime recovery process only if
  the exact current tmpfs generation is still present.
- `vault is missing required credentials`: run the relevant `vault-set`
  command; never put the value in `.env`.
- `Google OAuth refresh failed`: run `make gdrive-auth` again. Revoking the app
  grant in the Google account also invalidates the stored refresh token.
- `folder write check failed`: update the vault-backed folder URL/ID if needed,
  confirm the signed-in Google account, and verify that it can add children to
  the folder.
- checksum or AES-GCM authentication failure: do not restore that bundle.
  Preserve logs that contain no secrets and investigate corruption or a wrong
  `BACKUP_ENCRYPTION_KEY`.

## Credential rotation

- OAuth client secret: create a new Desktop client in Google Cloud, update
  `GDRIVE_CLIENT_ID` and `GDRIVE_CLIENT_SECRET`, then run `make gdrive-auth`.
- Refresh token: revoke the old app grant and run `make gdrive-auth`.
- Backup encryption key: old backups require the old key. Record a tested
  retention/migration plan before replacing `BACKUP_ENCRYPTION_KEY`.
- Vault passphrase: re-encrypt or recover into a new vault using the documented
  local-vault workflow; validate it before moving the old vault aside.

Useful Google references:

- [OAuth 2.0 for installed apps](https://developers.google.com/identity/protocols/oauth2/native-app)
- [Drive API scopes](https://developers.google.com/workspace/drive/api/guides/api-specific-auth)
- [Create files in folders](https://developers.google.com/workspace/drive/api/guides/folder)
- [Resumable uploads](https://developers.google.com/workspace/drive/api/guides/manage-uploads)
- [Drive file checksum fields](https://developers.google.com/workspace/drive/api/reference/rest/v3/files)
