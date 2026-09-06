-- Code-reviewed defaults; operators may install a project override as a NEW
-- version with a named owner. No background task may edit policy history.
CREATE TABLE data_quality_policies (
    project_id text NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    owner text NOT NULL CHECK (length(trim(owner)) > 0),
    effective_at timestamptz NOT NULL DEFAULT now(),
    source_max_age interval NOT NULL CHECK (source_max_age > interval '0'),
    content_max_age interval NOT NULL CHECK (content_max_age > interval '0'),
    patch_max_age interval NOT NULL CHECK (patch_max_age > interval '0'),
    projection_max_age interval NOT NULL CHECK (projection_max_age > interval '0'),
    PRIMARY KEY(project_id,version)
);
INSERT INTO data_quality_policies VALUES('*',1,'platform-maintainers',now(),interval '1 day',interval '30 days',interval '1 day',interval '1 day');
CREATE TRIGGER data_quality_policy_immutable BEFORE UPDATE OR DELETE ON data_quality_policies
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();

CREATE FUNCTION knowledge_quality_policy(project text) RETURNS SETOF data_quality_policies LANGUAGE sql STABLE AS $$
    SELECT * FROM data_quality_policies WHERE project_id IN ('*',project) AND effective_at<=now()
    ORDER BY (project_id=project) DESC,version DESC LIMIT 1;
$$;

CREATE TABLE knowledge_sources (
    validation_id uuid PRIMARY KEY REFERENCES knowledge_validations(id),
    manifest_sha256 char(64) NOT NULL REFERENCES artifacts(sha256),
    verified_at timestamptz NOT NULL,
    valid boolean NOT NULL,
    reason text NOT NULL
);
CREATE TABLE knowledge_projection_checks (
    knowledge_id uuid PRIMARY KEY REFERENCES knowledge_items(id),
    version integer NOT NULL,
    retrieval_sha256 char(64) NOT NULL,
    verified_at timestamptz NOT NULL
);
CREATE TABLE quality_review_cases (
    knowledge_id uuid NOT NULL REFERENCES knowledge_items(id),
    candidate_version integer NOT NULL,
    reason text NOT NULL,
    opened_at timestamptz NOT NULL DEFAULT now(),
    last_observed_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    PRIMARY KEY(knowledge_id,candidate_version,reason)
);

CREATE FUNCTION knowledge_quality_reason(item_id uuid) RETURNS text LANGUAGE sql STABLE AS $$
    SELECT CASE
      WHEN k.status<>'approved' THEN 'not_approved'
      WHEN p.version IS NULL THEN 'policy_unavailable'
      WHEN v.id IS NULL THEN 'validation_required'
      WHEN v.candidate_version<>k.version OR v.project_id<>k.project_id
        OR v.content_sha256<>encode(digest(k.content,'sha256'),'hex') THEN 'version_conflict'
      WHEN v.verdict<>'pass' OR v.completed_at>now() THEN 'validation_required'
      WHEN v.valid_until<=now() OR v.completed_at + CASE WHEN v.method='workpacket' THEN p.patch_max_age ELSE p.content_max_age END<=now() THEN 'validation_expired'
      WHEN s.validation_id IS NULL OR s.manifest_sha256<>v.source_manifest_sha256 THEN 'source_unverified'
      WHEN NOT s.valid THEN s.reason
      WHEN s.verified_at>now() OR s.verified_at+p.source_max_age<=now() THEN 'source_expired'
      WHEN length(trim(k.content))=0 OR length(trim(k.summary))=0 OR cardinality(k.procedure)=0 THEN 'content_incomplete'
      ELSE 'eligible' END
    FROM knowledge_items k LEFT JOIN knowledge_validations v ON v.id=k.approval_validation_id
    LEFT JOIN knowledge_sources s ON s.validation_id=v.id
    LEFT JOIN LATERAL knowledge_quality_policy(k.project_id) p ON true
    WHERE k.id=item_id;
$$;
CREATE FUNCTION knowledge_eligible(item_id uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT COALESCE(knowledge_quality_reason(item_id)='eligible',false);
$$;

ALTER TABLE outbox_events ADD COLUMN failed_at timestamptz;
CREATE INDEX outbox_failed_idx ON outbox_events(failed_at) WHERE failed_at IS NOT NULL;

CREATE FUNCTION knowledge_retrieval_fields_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.project_id,NEW.source_generation_id,NEW.workflow_id,NEW.workflow_step_id)
      IS DISTINCT FROM (OLD.project_id,OLD.source_generation_id,OLD.workflow_id,OLD.workflow_step_id) THEN
        RAISE EXCEPTION 'version_conflict: candidate identity and origin are immutable';
    END IF;
    IF (NEW.title,NEW.problem,NEW.summary,NEW.content,NEW.procedure,NEW.validation_evidence,NEW.task_type,NEW.language,NEW.tags,NEW.version)
      IS DISTINCT FROM (OLD.title,OLD.problem,OLD.summary,OLD.content,OLD.procedure,OLD.validation_evidence,OLD.task_type,OLD.language,OLD.tags,OLD.version) THEN
        IF OLD.status<>'pending' OR NEW.version<>OLD.version+1 THEN
            RAISE EXCEPTION 'version_conflict: retrieval changes require a new pending version';
        END IF;
        NEW.approval_validation_id:=NULL;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER knowledge_retrieval_fields_guard BEFORE UPDATE ON knowledge_items
FOR EACH ROW EXECUTE FUNCTION knowledge_retrieval_fields_guard();

CREATE FUNCTION knowledge_publication_quality_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status='approved' AND NOT knowledge_eligible(NEW.id) THEN
        RAISE EXCEPTION 'quality_blocked: publication requires current source, validation and content';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER knowledge_publication_quality_guard AFTER INSERT OR UPDATE ON knowledge_items
FOR EACH ROW EXECUTE FUNCTION knowledge_publication_quality_guard();
