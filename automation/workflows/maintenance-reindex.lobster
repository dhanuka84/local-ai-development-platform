name: hybrid-maintenance-reindex
steps:
  - id: diagnose
    command: make ops-doctor
  - id: approve
    command: /usr/bin/tee /dev/null
    stdin: $diagnose.stdout
    approval: required
  - id: reindex
    command: make ops-reindex
    condition: $approve.approved
