CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS projects (
    id text PRIMARY KEY,
    display_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS artifacts (
    sha256 char(64) PRIMARY KEY,
    uri text NOT NULL,
    media_type text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS generations (
    id uuid PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id),
    session_id text NOT NULL DEFAULT '',
    task_type text NOT NULL DEFAULT 'software-development',
    provider text NOT NULL DEFAULT '',
    model text NOT NULL DEFAULT '',
    repository_revision text NOT NULL DEFAULT '',
    outcome text NOT NULL DEFAULT 'unknown',
    procedure jsonb NOT NULL DEFAULT '[]',
    validation_evidence jsonb NOT NULL DEFAULT '[]',
    prompt_artifact_sha256 char(64) NOT NULL REFERENCES artifacts(sha256),
    output_artifact_sha256 char(64) NOT NULL REFERENCES artifacts(sha256),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS knowledge_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id text NOT NULL REFERENCES projects(id),
    title text NOT NULL,
    problem text NOT NULL,
    summary text NOT NULL DEFAULT '',
    content text NOT NULL,
    procedure text[] NOT NULL DEFAULT '{}',
    validation_evidence text[] NOT NULL DEFAULT '{}',
    task_type text NOT NULL DEFAULT 'software-development',
    language text NOT NULL DEFAULT '',
    tags text[] NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'deprecated')),
    source_generation_id uuid REFERENCES generations(id),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    created_by text NOT NULL DEFAULT 'capture',
    approved_at timestamptz,
    approved_by text,
    search_document tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(problem, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(summary, '')), 'B') ||
        setweight(to_tsvector('simple', coalesce(content, '')), 'C')
    ) STORED
);

CREATE INDEX IF NOT EXISTS knowledge_items_project_status_idx
    ON knowledge_items(project_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS knowledge_items_search_idx
    ON knowledge_items USING gin(search_document);
CREATE INDEX IF NOT EXISTS knowledge_items_tags_idx
    ON knowledge_items USING gin(tags);

CREATE TABLE IF NOT EXISTS knowledge_relations (
    from_id uuid NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
    to_id uuid NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
    relation_type text NOT NULL,
    confidence real NOT NULL DEFAULT 1 CHECK (confidence >= 0 AND confidence <= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (from_id, to_id, relation_type)
);

CREATE TABLE IF NOT EXISTS review_records (
    id uuid PRIMARY KEY,
    knowledge_id uuid NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
    reviewer text NOT NULL,
    provider text NOT NULL DEFAULT '',
    model text NOT NULL DEFAULT '',
    verdict text NOT NULL CHECK (verdict IN ('approve', 'reject', 'revise', 'comment')),
    comments text NOT NULL DEFAULT '',
    improved_content text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS review_records_knowledge_idx
    ON review_records(knowledge_id, created_at DESC);

CREATE TABLE IF NOT EXISTS software_repositories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id text NOT NULL REFERENCES projects(id),
    name text NOT NULL,
    canonical_url text NOT NULL,
    default_branch text NOT NULL DEFAULT '',
    revision text NOT NULL DEFAULT '',
    metadata jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, canonical_url)
);

CREATE TABLE IF NOT EXISTS repository_relations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id text NOT NULL REFERENCES projects(id),
    from_repository_id uuid NOT NULL REFERENCES software_repositories(id) ON DELETE CASCADE,
    to_repository_id uuid NOT NULL REFERENCES software_repositories(id) ON DELETE CASCADE,
    relation_type text NOT NULL CHECK (relation_type IN (
        'depends_on', 'provides_api_to', 'deploys_with', 'shares_contract',
        'fork_of', 'upstream_of', 'successor_of', 'contains', 'related_to'
    )),
    evidence text NOT NULL,
    confidence real NOT NULL DEFAULT 1 CHECK (confidence >= 0 AND confidence <= 1),
    approved_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, from_repository_id, to_repository_id, relation_type),
    CHECK (from_repository_id <> to_repository_id)
);

CREATE INDEX IF NOT EXISTS repository_relations_from_idx ON repository_relations(from_repository_id);
CREATE INDEX IF NOT EXISTS repository_relations_to_idx ON repository_relations(to_repository_id);

CREATE TABLE IF NOT EXISTS outbox_events (
    id bigserial PRIMARY KEY,
    aggregate_id uuid NOT NULL,
    topic text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    completed_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS outbox_pending_idx
    ON outbox_events(next_attempt_at, id)
    WHERE completed_at IS NULL;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
