CREATE TABLE IF NOT EXISTS code_analysis_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id text NOT NULL REFERENCES projects(id),
    repository_id uuid NOT NULL REFERENCES software_repositories(id) ON DELETE CASCADE,
    revision text NOT NULL,
    analyzer text NOT NULL,
    analyzer_version text NOT NULL,
    requested_by text NOT NULL,
    status text NOT NULL DEFAULT 'succeeded' CHECK (status IN ('succeeded', 'failed')),
    statistics jsonb NOT NULL DEFAULT '{}',
    started_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL,
    error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (completed_at >= started_at)
);

CREATE INDEX IF NOT EXISTS code_analysis_runs_repository_idx
    ON code_analysis_runs(repository_id, created_at DESC);

CREATE TABLE IF NOT EXISTS code_entities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id text NOT NULL REFERENCES projects(id),
    repository_id uuid NOT NULL REFERENCES software_repositories(id) ON DELETE CASCADE,
    stable_key text NOT NULL,
    language text NOT NULL,
    kind text NOT NULL CHECK (kind IN (
        'repository', 'module', 'package', 'file', 'type', 'interface',
        'function', 'method', 'field', 'variable', 'constant', 'test'
    )),
    name text NOT NULL,
    qualified_name text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    search_document tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(name, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(qualified_name, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(kind, '')), 'B')
    ) STORED,
    UNIQUE (repository_id, stable_key)
);

CREATE INDEX IF NOT EXISTS code_entities_project_kind_idx
    ON code_entities(project_id, kind, qualified_name);
CREATE INDEX IF NOT EXISTS code_entities_search_idx
    ON code_entities USING gin(search_document);

CREATE TABLE IF NOT EXISTS code_occurrences (
    analysis_run_id uuid NOT NULL REFERENCES code_analysis_runs(id) ON DELETE CASCADE,
    entity_id uuid NOT NULL REFERENCES code_entities(id) ON DELETE CASCADE,
    file_path text NOT NULL DEFAULT '',
    start_line integer NOT NULL DEFAULT 0 CHECK (start_line >= 0),
    start_column integer NOT NULL DEFAULT 0 CHECK (start_column >= 0),
    end_line integer NOT NULL DEFAULT 0 CHECK (end_line >= 0),
    end_column integer NOT NULL DEFAULT 0 CHECK (end_column >= 0),
    signature text NOT NULL DEFAULT '',
    content_hash char(64),
    metadata jsonb NOT NULL DEFAULT '{}',
    PRIMARY KEY (analysis_run_id, entity_id)
);

CREATE INDEX IF NOT EXISTS code_occurrences_entity_idx
    ON code_occurrences(entity_id, analysis_run_id);
CREATE INDEX IF NOT EXISTS code_occurrences_file_idx
    ON code_occurrences(analysis_run_id, file_path);

CREATE TABLE IF NOT EXISTS code_relations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    analysis_run_id uuid NOT NULL REFERENCES code_analysis_runs(id) ON DELETE CASCADE,
    source_entity_id uuid NOT NULL REFERENCES code_entities(id) ON DELETE CASCADE,
    target_entity_id uuid NOT NULL REFERENCES code_entities(id) ON DELETE CASCADE,
    relation_type text NOT NULL CHECK (relation_type IN (
        'contains', 'defines', 'imports', 'calls', 'references',
        'implements', 'embeds', 'tests'
    )),
    evidence text NOT NULL DEFAULT '',
    confidence real NOT NULL DEFAULT 1 CHECK (confidence >= 0 AND confidence <= 1),
    file_path text NOT NULL DEFAULT '',
    start_line integer NOT NULL DEFAULT 0 CHECK (start_line >= 0),
    start_column integer NOT NULL DEFAULT 0 CHECK (start_column >= 0),
    end_line integer NOT NULL DEFAULT 0 CHECK (end_line >= 0),
    end_column integer NOT NULL DEFAULT 0 CHECK (end_column >= 0),
    metadata jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (
        analysis_run_id, source_entity_id, target_entity_id, relation_type,
        file_path, start_line, start_column
    )
);

CREATE INDEX IF NOT EXISTS code_relations_source_idx
    ON code_relations(analysis_run_id, source_entity_id, relation_type);
CREATE INDEX IF NOT EXISTS code_relations_target_idx
    ON code_relations(analysis_run_id, target_entity_id, relation_type);

CREATE TABLE IF NOT EXISTS code_repository_heads (
    repository_id uuid PRIMARY KEY REFERENCES software_repositories(id) ON DELETE CASCADE,
    analysis_run_id uuid NOT NULL REFERENCES code_analysis_runs(id) ON DELETE CASCADE,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS knowledge_code_references (
    knowledge_id uuid NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
    entity_id uuid NOT NULL REFERENCES code_entities(id) ON DELETE CASCADE,
    analysis_run_id uuid NOT NULL REFERENCES code_analysis_runs(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN (
        'used_context', 'applies_to', 'modifies', 'validates', 'review_concern'
    )),
    evidence text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (knowledge_id, entity_id, analysis_run_id, role)
);
