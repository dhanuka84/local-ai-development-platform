# Scoped local task credentials

An authenticated human developer can issue a short-lived credential for one
existing active task. The delegate receives only the intersection with the
issuer's current development authority for that project. It is always a
workload identity, with durable attribution to the human and exact parent
credential. It receives no QA, Product Owner, validation-executor, controller,
operations or cloud-review role.

The operator CLI writes the random 256-bit bearer token directly to an exclusive
mode-0600 file inside an existing mode-0700 directory. It prints only delegation
metadata. Issuance is deliberately absent from MCP so a model does not receive
the credential in a tool response. PostgreSQL stores only its SHA-256 hash.

```sh
mkdir -m 700 /dev/shm/local-task-session
bin/admin delegate-task TASK_UUID 900 /dev/shm/local-task-session/token
bin/admin revoke-task-delegation DELEGATION_UUID
```

The CLI needs the existing human operator credential, database configuration
and Cerbos endpoint. Keep the token file in the local supervisor's credential
injection path; never paste its contents into a prompt, shell argument, log,
issue or review. File creation refuses existing files and symlinks. A failed
write attempts revocation before returning an error. Lost delivery cannot be
retried to reveal the original token: revoke its metadata ID and issue a new
credential.

TTL is 60–3,600 seconds and is capped at the parent credential's expiry. Every
HTTP request authenticates against PostgreSQL. The MCP tool boundary rechecks
delegation validity, including contexts established before revocation. Parent
revocation, expiry, deletion/rotation, inactive identity, loss of development
authority, delegation revocation, completed/blocked/rejected task or terminal
workflow prevents subsequent calls. Revocation does not undo previously
committed actions or cancel a request already executing inside a handler.

The delegate can read eligible project knowledge, repository/code context and
approved metrics, and read its own workflow/task. It can capture local Ollama
proposals, record actual context use, revise its assigned pending candidate,
and request the allowed local checkpoints for that task. Attaching a newly
captured candidate requires its immutable capture receipt to match both this
delegate and task. It cannot attach another task's candidate, create another
task/workflow, publish knowledge, attest as human QA, request cloud review,
change context definitions, retry operational indexes, or acquire additional
credentials. New tools are denied until explicitly added to the allowlist.

Cerbos, source eligibility, version checks and executed-validation requirements
still run after this additional boundary. A trusted validation-executor performs
local patch verification; the developer credential does not mint that role.
The token bounds platform APIs, not arbitrary host processes. The supervisor
must retain work-packet scope, local-model routing and container/network limits.
Privileged database administration remains a separate trusted operator path.

Migration `000018_task_delegations.sql` preserves immutable scope and issuer
attribution while allowing one-way revocation. Credential issuance/revocation
and operation evidence commit in the same PostgreSQL transaction. Denied tool
attempts record only the delegate's authorized task scope, not a requested
foreign resource's metadata. Existing environment bootstrap rotates parent
credential rows on startup, which invalidates outstanding derived credentials.

The full disposable acceptance includes real PostgreSQL and Cerbos checks for
role attenuation, task/candidate scope, recursive delegation, TTL, parent and
delegate revocation, stale request contexts, lost parent authority, terminal
tasks, immutable attribution, token non-disclosure and failed-evidence rollback.
It uses synthetic identities and does not issue a live user delegation.
