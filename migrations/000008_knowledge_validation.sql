CREATE TABLE knowledge_validations (
    id uuid PRIMARY KEY,
    knowledge_id uuid NOT NULL REFERENCES knowledge_items(id),
    project_id text NOT NULL REFERENCES projects(id),
    candidate_version integer NOT NULL CHECK (candidate_version > 0),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
    source_manifest_sha256 char(64) NOT NULL REFERENCES artifacts(sha256),
    report_artifact_sha256 char(64) NOT NULL REFERENCES artifacts(sha256),
    method text NOT NULL CHECK (method IN ('manual','workpacket')),
    verdict text NOT NULL CHECK (verdict IN ('pass','fail')),
    validated_by text NOT NULL REFERENCES principals(id),
    started_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL,
    valid_until timestamptz NOT NULL,
    report jsonb NOT NULL,
    CHECK (completed_at >= started_at AND valid_until > completed_at),
    CHECK (valid_until <= completed_at + interval '30 days')
);
CREATE INDEX knowledge_validations_candidate_idx ON knowledge_validations(knowledge_id,candidate_version,completed_at DESC);

ALTER TABLE knowledge_items ADD COLUMN approval_validation_id uuid REFERENCES knowledge_validations(id);

CREATE TABLE knowledge_decisions (
    id uuid PRIMARY KEY,
    knowledge_id uuid NOT NULL REFERENCES knowledge_items(id),
    candidate_version integer NOT NULL,
    validation_id uuid REFERENCES knowledge_validations(id),
    decision text NOT NULL CHECK (decision IN ('approve','reject')),
    reason text NOT NULL CHECK (length(trim(reason)) > 0),
    actor text NOT NULL REFERENCES principals(id),
    idempotency_key text NOT NULL,
    request_sha256 text NOT NULL,
    policy_decision jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(knowledge_id,idempotency_key),
    CHECK (decision <> 'approve' OR validation_id IS NOT NULL)
);

CREATE FUNCTION knowledge_evidence_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'knowledge evidence is append-only';
END;
$$;
CREATE TRIGGER knowledge_validation_immutable BEFORE UPDATE OR DELETE ON knowledge_validations
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TRIGGER knowledge_decision_immutable BEFORE UPDATE OR DELETE ON knowledge_decisions
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();

CREATE FUNCTION knowledge_promotion_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.content,NEW.procedure,NEW.summary,NEW.title,NEW.problem)
      IS DISTINCT FROM (OLD.content,OLD.procedure,OLD.summary,OLD.title,OLD.problem) THEN
        IF OLD.status <> 'pending' OR NEW.version <> OLD.version + 1 THEN
            RAISE EXCEPTION 'version_conflict: content changes require a new pending version';
        END IF;
        NEW.approval_validation_id := NULL;
    END IF;
    IF NEW.status = 'approved' AND (TG_OP = 'INSERT' OR OLD.status IS DISTINCT FROM 'approved'
      OR NEW.approval_validation_id IS DISTINCT FROM OLD.approval_validation_id) THEN
        IF NOT EXISTS (
            SELECT 1 FROM knowledge_validations v JOIN principals p ON p.id=NEW.approved_by
            JOIN knowledge_decisions d ON d.knowledge_id=NEW.id AND d.validation_id=v.id
              AND d.candidate_version=NEW.version AND d.decision='approve' AND d.actor=p.id
            WHERE v.id=NEW.approval_validation_id AND v.knowledge_id=NEW.id
              AND v.project_id=NEW.project_id AND v.candidate_version=NEW.version
              AND v.content_sha256=encode(digest(NEW.content,'sha256'),'hex')
              AND v.verdict='pass' AND v.valid_until>now() AND v.completed_at<=now()
              AND p.kind='human' AND p.active
        ) THEN
            RAISE EXCEPTION 'validation_required: exact-version evidence and human decision required';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER knowledge_promotion_guard BEFORE INSERT OR UPDATE ON knowledge_items
FOR EACH ROW EXECUTE FUNCTION knowledge_promotion_guard();
