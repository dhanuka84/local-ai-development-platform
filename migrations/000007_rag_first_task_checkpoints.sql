CREATE TABLE IF NOT EXISTS workflow_task_checkpoints (
    id uuid PRIMARY KEY,
    workflow_id uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    ordinal integer NOT NULL CHECK (ordinal > 0),
    task_key text NOT NULL,
    title text NOT NULL,
    task_type text NOT NULL DEFAULT 'implementation',
    state text NOT NULL DEFAULT 'queued' CHECK (state IN (
        'queued', 'local_execution', 'review_approval_required', 'cloud_review_required', 'local_revision_required',
        'validation_required', 'promotion_required', 'rag_readback_required',
        'completed', 'blocked', 'rejected'
    )),
    route text NOT NULL CHECK (route IN (
        'pending', 'rag_hit', 'rag_miss_cloud_review', 'rag_miss_local_only'
    )),
    execution_mode text NOT NULL DEFAULT 'auto' CHECK (execution_mode IN ('auto', 'manual')),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    rag_query text NOT NULL,
    rag_backend text NOT NULL,
    rag_hit_ids text[] NOT NULL DEFAULT '{}',
    rag_max_score real NOT NULL DEFAULT 0,
    match_threshold real NOT NULL DEFAULT 0.75 CHECK (match_threshold >= 0 AND match_threshold <= 1),
    review_influence_weight real NOT NULL DEFAULT 0 CHECK (review_influence_weight >= 0 AND review_influence_weight <= 1),
    candidate_id uuid REFERENCES knowledge_items(id),
    local_provider text NOT NULL DEFAULT '',
    local_model text NOT NULL DEFAULT '',
    cloud_provider text NOT NULL DEFAULT '',
    cloud_model text NOT NULL DEFAULT '',
    request_artifact_sha256 char(64) NOT NULL REFERENCES artifacts(sha256),
    lookup_artifact_sha256 char(64) REFERENCES artifacts(sha256),
    created_by text NOT NULL REFERENCES principals(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (workflow_id, ordinal),
    UNIQUE (workflow_id, task_key)
);

CREATE UNIQUE INDEX IF NOT EXISTS workflow_task_checkpoints_candidate_idx
    ON workflow_task_checkpoints(candidate_id) WHERE candidate_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS workflow_task_checkpoints_workflow_state_idx
    ON workflow_task_checkpoints(workflow_id, state, ordinal);

CREATE TABLE IF NOT EXISTS workflow_task_events (
    id uuid PRIMARY KEY,
    task_id uuid NOT NULL REFERENCES workflow_task_checkpoints(id) ON DELETE CASCADE,
    sequence integer NOT NULL CHECK (sequence > 0),
    event_type text NOT NULL,
    from_state text NOT NULL,
    to_state text NOT NULL,
    actor_principal_id text NOT NULL REFERENCES principals(id),
    actor_role text NOT NULL,
    provider text NOT NULL DEFAULT '',
    model text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}',
    evidence_artifact_sha256 char(64) REFERENCES artifacts(sha256),
    authorization_decision text NOT NULL CHECK (authorization_decision IN ('allow', 'deny')),
    cerbos_call_id text NOT NULL DEFAULT '',
    policy_version text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (task_id, sequence),
    UNIQUE (task_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS workflow_task_events_task_idx
    ON workflow_task_events(task_id, sequence);
