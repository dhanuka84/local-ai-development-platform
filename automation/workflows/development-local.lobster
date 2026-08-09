name: hybrid-development-local
args:
  packet:
    required: true
  patch:
    required: true
steps:
  - id: evaluate
    command: ./bin/workpacket evaluate --packet "$LOBSTER_ARG_PACKET"
  - id: verify
    command: ./bin/workpacket verify --packet "$LOBSTER_ARG_PACKET" --patch "$LOBSTER_ARG_PATCH"
    stdin: $evaluate.json
