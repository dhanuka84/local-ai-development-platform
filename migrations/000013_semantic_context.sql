CREATE TABLE context_registry_validations (
    id uuid PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id),
    registry_sha256 char(64) NOT NULL,
    actor text NOT NULL REFERENCES principals(id),
    evidence_sha256 char(64) NOT NULL REFERENCES artifacts(sha256),
    completed_at timestamptz NOT NULL
);
CREATE TRIGGER context_validation_immutable BEFORE UPDATE OR DELETE ON context_registry_validations FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TABLE context_definitions (
    project_id text NOT NULL REFERENCES projects(id),
    id text NOT NULL,
    version integer NOT NULL CHECK(version>0),
    kind text NOT NULL CHECK(kind IN ('metric','domain','capability')),
    definition jsonb NOT NULL,
    sha256 char(64) NOT NULL,
    registry_sha256 char(64) NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
    validated_at timestamptz NOT NULL,
    approved_by text REFERENCES principals(id),
    approved_at timestamptz,
    projected_at timestamptz,
    projection_model text NOT NULL DEFAULT '',
    projection_dimension integer NOT NULL DEFAULT 0,
    PRIMARY KEY(project_id,id,version)
);
CREATE TABLE context_definition_decisions (
    id uuid PRIMARY KEY,
    project_id text NOT NULL,
    definition_id text NOT NULL,
    version integer NOT NULL,
    validation_id uuid NOT NULL REFERENCES context_registry_validations(id),
    actor text NOT NULL REFERENCES principals(id),
    decision text NOT NULL CHECK(decision IN ('approve','reject')),
    reason text NOT NULL,
    idempotency_key text NOT NULL,
    request_sha256 char(64) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(project_id,definition_id,idempotency_key),
    FOREIGN KEY(project_id,definition_id,version) REFERENCES context_definitions(project_id,id,version)
);
CREATE TRIGGER context_decision_immutable BEFORE UPDATE OR DELETE ON context_definition_decisions FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE FUNCTION context_definition_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND (NEW.definition,NEW.sha256,NEW.registry_sha256,NEW.id,NEW.version,NEW.project_id)
      IS DISTINCT FROM (OLD.definition,OLD.sha256,OLD.registry_sha256,OLD.id,OLD.version,OLD.project_id) THEN
        RAISE EXCEPTION 'version_conflict: definition revisions are immutable';
    END IF;
    IF NEW.status='approved' AND (TG_OP='INSERT' OR OLD.status<>'approved') AND NOT EXISTS(
        SELECT 1 FROM context_definition_decisions d JOIN principals p ON p.id=d.actor
        JOIN context_registry_validations v ON v.id=d.validation_id
        WHERE d.project_id=NEW.project_id AND d.definition_id=NEW.id AND d.version=NEW.version
        AND d.decision='approve' AND p.id=NEW.approved_by AND p.kind='human' AND p.active
        AND v.project_id=NEW.project_id AND v.registry_sha256=NEW.registry_sha256
        AND v.completed_at+interval '30 days'>now()) THEN
        RAISE EXCEPTION 'validation_required: context approval needs a human decision and registry evidence';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER context_definition_guard BEFORE INSERT OR UPDATE ON context_definitions FOR EACH ROW EXECUTE FUNCTION context_definition_guard();

CREATE TABLE workflow_task_used_context (
    task_id uuid NOT NULL REFERENCES workflow_task_checkpoints(id),
    knowledge_id uuid NOT NULL REFERENCES knowledge_items(id),
    candidate_version integer NOT NULL,
    validation_id uuid NOT NULL REFERENCES knowledge_validations(id),
    source_manifest_sha256 char(64) NOT NULL,
    target_repository text NOT NULL DEFAULT '',
    target_branch text NOT NULL DEFAULT '',
    target_revision text NOT NULL DEFAULT '',
    actor text NOT NULL REFERENCES principals(id),
    idempotency_key text NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(task_id,knowledge_id),
    UNIQUE(task_id,idempotency_key,knowledge_id)
);
CREATE TRIGGER used_context_immutable BEFORE UPDATE OR DELETE ON workflow_task_used_context FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TABLE workflow_task_local_validations (
    task_id uuid NOT NULL REFERENCES workflow_task_checkpoints(id),
    validation_id uuid NOT NULL REFERENCES knowledge_validations(id),
    candidate_version integer NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(task_id,validation_id)
);
CREATE TRIGGER task_validation_immutable BEFORE UPDATE OR DELETE ON workflow_task_local_validations FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
