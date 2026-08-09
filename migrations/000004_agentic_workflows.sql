CREATE TABLE IF NOT EXISTS principals (
    id text PRIMARY KEY,
    display_name text NOT NULL DEFAULT '',
    kind text NOT NULL CHECK (kind IN ('human', 'workload')),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS principal_credentials (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    principal_id text NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    token_sha256 bytea NOT NULL UNIQUE CHECK (octet_length(token_sha256) = 32),
    label text NOT NULL DEFAULT 'local-token',
    expires_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS principal_credentials_principal_idx
    ON principal_credentials(principal_id) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS principal_role_bindings (
    principal_id text NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    project_id text NOT NULL,
    role text NOT NULL CHECK (role IN (
        'controller', 'development', 'qa', 'product_owner', 'operations',
        'cloud_reviewer', 'maintenance_executor', 'repository_analyzer'
    )),
    valid_from timestamptz NOT NULL DEFAULT now(),
    valid_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, project_id, role)
);

CREATE INDEX IF NOT EXISTS principal_role_bindings_project_idx
    ON principal_role_bindings(project_id, role);

CREATE TABLE IF NOT EXISTS project_governance_policies (
    project_id text PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    profile text NOT NULL DEFAULT 'solo' CHECK (profile IN ('solo', 'team', 'regulated')),
    allow_role_overlap boolean NOT NULL DEFAULT true,
    distinct_principal_gates text[] NOT NULL DEFAULT '{}',
    two_person_gates text[] NOT NULL DEFAULT '{}',
    always_human_gates text[] NOT NULL DEFAULT ARRAY[
        'plan_approval','qa','product_approval','confidential_disclosure','maintenance_action'
    ],
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_by text REFERENCES principals(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workflow_runs (
    id uuid PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id),
    kind text NOT NULL DEFAULT 'software-development',
    state text NOT NULL DEFAULT 'intake' CHECK (state IN (
        'intake', 'needs_clarification', 'classified', 'policy_rejected',
        'awaiting_plan_approval', 'ready', 'implementing',
        'implementation_failed', 'verifying', 'revision_required',
        'review_packaging', 'awaiting_disclosure_approval', 'reviewing',
        'reconciling', 'qa_validating', 'qa_failed', 'awaiting_human_qa',
        'qa_validated', 'awaiting_product_approval', 'product_rejected',
        'promotion_pending', 'completed', 'blocked', 'cancel_requested',
        'cancelled', 'failed'
    )),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    risk text NOT NULL DEFAULT 'low' CHECK (risk IN ('low', 'medium', 'high', 'critical')),
    data_classification text NOT NULL DEFAULT 'internal' CHECK (data_classification IN (
        'public', 'internal', 'confidential', 'restricted'
    )),
    request_artifact_sha256 char(64) NOT NULL REFERENCES artifacts(sha256),
    work_packet_artifact_sha256 char(64) REFERENCES artifacts(sha256),
    openclaw_flow_id text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL,
    created_by text NOT NULL REFERENCES principals(id),
    implemented_by text REFERENCES principals(id),
    qa_validated_by text REFERENCES principals(id),
    metadata jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    terminal_at timestamptz,
    UNIQUE (project_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS workflow_runs_project_state_idx
    ON workflow_runs(project_id, state, updated_at DESC);

CREATE TABLE IF NOT EXISTS workflow_steps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_key text NOT NULL,
    attempt integer NOT NULL DEFAULT 1 CHECK (attempt > 0),
    role text NOT NULL,
    agent_id text NOT NULL DEFAULT '',
    provider text NOT NULL DEFAULT '',
    model text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'pending' CHECK (state IN (
        'pending', 'running', 'waiting', 'completed', 'failed', 'cancelled'
    )),
    input_artifact_sha256 char(64) REFERENCES artifacts(sha256),
    output_artifact_sha256 char(64) REFERENCES artifacts(sha256),
    started_at timestamptz,
    completed_at timestamptz,
    error_code text NOT NULL DEFAULT '',
    UNIQUE (workflow_id, step_key, attempt)
);

CREATE TABLE IF NOT EXISTS workflow_approvals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    attempt integer NOT NULL DEFAULT 1 CHECK (attempt > 0),
    gate text NOT NULL,
    requested_role text NOT NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'approved', 'rejected', 'expired', 'cancelled')),
    requested_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    preview_artifact_sha256 char(64) REFERENCES artifacts(sha256),
    decided_by text REFERENCES principals(id),
    decided_at timestamptz,
    decision text NOT NULL DEFAULT '',
    reason text NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS workflow_approvals_active_idx
    ON workflow_approvals(workflow_id, gate, attempt) WHERE state = 'pending';

CREATE TABLE IF NOT EXISTS workflow_events (
    id uuid PRIMARY KEY,
    workflow_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    sequence integer NOT NULL CHECK (sequence > 0),
    event_type text NOT NULL,
    from_state text NOT NULL,
    to_state text NOT NULL,
    actor_principal_id text NOT NULL REFERENCES principals(id),
    actor_role text NOT NULL,
    idempotency_key text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}',
    evidence_artifact_sha256 char(64) REFERENCES artifacts(sha256),
    authorization_decision text NOT NULL CHECK (authorization_decision IN ('allow', 'deny')),
    cerbos_call_id text NOT NULL DEFAULT '',
    policy_version text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, sequence),
    UNIQUE (workflow_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS workflow_events_workflow_idx
    ON workflow_events(workflow_id, sequence);

ALTER TABLE generations
    ADD COLUMN IF NOT EXISTS workflow_id uuid REFERENCES workflow_runs(id),
    ADD COLUMN IF NOT EXISTS workflow_step_id uuid REFERENCES workflow_steps(id);

ALTER TABLE review_records
    ADD COLUMN IF NOT EXISTS workflow_id uuid REFERENCES workflow_runs(id),
    ADD COLUMN IF NOT EXISTS workflow_step_id uuid REFERENCES workflow_steps(id);

ALTER TABLE knowledge_items
    ADD COLUMN IF NOT EXISTS workflow_id uuid REFERENCES workflow_runs(id),
    ADD COLUMN IF NOT EXISTS workflow_step_id uuid REFERENCES workflow_steps(id);
