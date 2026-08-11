CREATE TABLE IF NOT EXISTS graph_repository_projection_heads (
    repository_id uuid PRIMARY KEY REFERENCES software_repositories(id) ON DELETE CASCADE,
    revision text NOT NULL DEFAULT '',
    backend text NOT NULL DEFAULT 'apache-age',
    source_updated_at timestamptz NOT NULL,
    projected_at timestamptz NOT NULL DEFAULT now(),
    status text NOT NULL DEFAULT 'ready' CHECK (status IN ('projecting', 'ready', 'failed')),
    last_error text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS graph_projection_relations (
    relation_id uuid PRIMARY KEY REFERENCES repository_relations(id) ON DELETE CASCADE,
    backend text NOT NULL DEFAULT 'apache-age',
    source_updated_at timestamptz NOT NULL,
    projected_at timestamptz NOT NULL DEFAULT now(),
    status text NOT NULL DEFAULT 'ready' CHECK (status IN ('projecting', 'ready', 'failed')),
    last_error text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS graph_projection_heads (
    repository_id uuid PRIMARY KEY REFERENCES software_repositories(id) ON DELETE CASCADE,
    analysis_run_id uuid NOT NULL REFERENCES code_analysis_runs(id) ON DELETE CASCADE,
    revision text NOT NULL,
    backend text NOT NULL DEFAULT 'apache-age',
    projected_at timestamptz NOT NULL DEFAULT now(),
    status text NOT NULL DEFAULT 'ready' CHECK (status IN ('projecting', 'ready', 'failed')),
    last_error text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS graph_knowledge_projection_heads (
    knowledge_id uuid PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
    version integer NOT NULL,
    backend text NOT NULL DEFAULT 'apache-age',
    projected_at timestamptz NOT NULL DEFAULT now(),
    status text NOT NULL DEFAULT 'ready' CHECK (status IN ('projecting', 'ready', 'failed')),
    last_error text NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS graph_repository_projection_status_idx
    ON graph_repository_projection_heads(status, projected_at);
CREATE INDEX IF NOT EXISTS graph_projection_relations_status_idx
    ON graph_projection_relations(status, projected_at);
CREATE INDEX IF NOT EXISTS graph_projection_heads_status_idx
    ON graph_projection_heads(status, projected_at);
CREATE INDEX IF NOT EXISTS graph_knowledge_projection_status_idx
    ON graph_knowledge_projection_heads(status, projected_at);
