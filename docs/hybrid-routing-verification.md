# Hybrid Routing Verification

## Outcome

`make hybrid-verify` proves that the local-development and cloud-review lanes
fail closed. It runs against a temporary canary Git repository, so the
verification does not disclose the platform repository or another product
repository to the cloud reviewer.

This is the infrastructure acceptance test for default task
`execution_mode=auto`: no human accepts the cloud-review invocation, so the
network and sandbox controls themselves must fail closed. It does not remove
the later knowledge-promotion approval.

The suite does not stop or reconfigure the shared Ollama container. This makes
it safe to run while another Codex session is using the local model.

## Run the complete verification

Start the platform and authenticate Codex first, then run:

```bash
make hybrid-verify
```

The target builds a fresh `alpine:3.24.0` runner by default. Override only when
diagnosing a controlled environment:

```bash
make hybrid-verify \
  CODEX_REVIEW_MODEL=gpt-5.6-sol \
  HYBRID_VERIFY_TIMEOUT=240 \
  HYBRID_VERIFY_BUILD_FLAGS=--pull
```

## What it proves

| Test | Cloud route | Ollama route | Required result |
|---|---|---|---|
| Local development with cloud denied | Denied by an internal Docker network | Allowed through a fixed Ollama-only proxy | Local inference succeeds and reports the local provider. |
| Local development with Ollama unavailable | Confirmed reachable and authenticated | A per-process provider points to an unreachable endpoint | Local inference fails without switching to cloud. |
| Cloud review write attempt | Allowed | Not selected | `codex review` succeeds, the adversarial write is absent, and the working-tree hash is unchanged. |
| Cloud review with cloud denied | Denied by an internal Docker network | Deliberately left reachable | Review fails without switching to Ollama. |

The isolated runner has no general egress route. A dual-network `socat`
sidecar forwards only to the existing Ollama service. The suite also performs
an HTTPS probe and requires the ChatGPT destination to be unreachable from the
local-only runner.

The unavailable-Ollama case uses a custom Codex provider pointed at
`127.0.0.1:1`. It does not stop the shared Ollama container. Cloud
reachability is confirmed immediately before this negative test.

The review case uses the dedicated `codex review` command with:

- the selected cloud model;
- `--sandbox read-only`;
- `--ask-for-approval never`;
- all MCP servers disabled for the review process;
- a temporary canary diff containing no project source.

Codex documents `read-only` as a supported sandbox mode and `codex review` as
the non-interactive review command. See the
[Codex developer-command reference](https://learn.chatgpt.com/docs/developer-commands?surface=cli)
and [Codex code-review workflow](https://learn.chatgpt.com/docs/code-review).

## Audit evidence

Every model invocation produces one JSON object in:

```text
reports/hybrid-verification/<run-id>/audit.jsonl
```

The same directory contains a mode-`0600` raw log for each test. Each audit
object records:

- role (`development` or `review`);
- provider and model;
- canary repository name, branch, and commit;
- UTC start and completion timestamps;
- selected network destination and enforced network policy;
- SHA-256 of the input diff;
- working-tree SHA-256 before and after the invocation;
- process exit code and expected result;
- evidence filename and SHA-256;
- test-specific assertions.

The final phase validates all required fields, requires exactly four records,
and fails unless every record has status `pass`. `reports/` is ignored by Git,
but the evidence may still contain model output and should follow the local
retention policy.

## Safety and cleanup

The suite creates uniquely named temporary containers and an internal Docker
network. A trap removes only those exact resources and the uniquely generated
temporary directory. It does not remove platform containers, images, volumes,
repositories, or existing Codex sessions.

The cloud-denial test copies only the file-backed Codex authentication record
into the isolated temporary directory. That container has no cloud route. The
temporary copy is removed during cleanup and is never written to the audit
record.
