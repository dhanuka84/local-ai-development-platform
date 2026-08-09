name: hybrid-qa-validation
args:
  packet:
    required: true
  patch:
    required: true
steps:
  - id: verify
    command: ./bin/workpacket verify --packet "$LOBSTER_ARG_PACKET" --patch "$LOBSTER_ARG_PATCH"
  - id: human-qa
    command: /usr/bin/tee /dev/null
    stdin: $verify.json
    approval: required
