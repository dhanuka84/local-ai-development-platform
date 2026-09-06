ALTER TABLE data_quality_policies ADD COLUMN data_product text NOT NULL DEFAULT '*';
ALTER TABLE data_quality_policies ADD COLUMN purpose text NOT NULL DEFAULT '*'
    CHECK(purpose IN ('*','development_guidance','code_change','review'));
ALTER TABLE data_quality_policies ADD COLUMN required_method text NOT NULL DEFAULT 'any' CHECK(required_method IN ('any','workpacket'));
ALTER TABLE data_quality_policies ADD COLUMN max_content_bytes integer NOT NULL DEFAULT 2097152 CHECK(max_content_bytes BETWEEN 1 AND 8388608);
ALTER TABLE data_quality_policies DROP CONSTRAINT data_quality_policies_pkey;
ALTER TABLE data_quality_policies ADD PRIMARY KEY(project_id,data_product,purpose,version);
CREATE FUNCTION knowledge_quality_policy(project text,product text,consumer text) RETURNS SETOF data_quality_policies LANGUAGE sql STABLE AS $$
    SELECT * FROM data_quality_policies WHERE project_id IN ('*',project) AND data_product IN ('*',product)
      AND purpose IN ('*',consumer) AND effective_at<=now()
    ORDER BY (project_id=project) DESC,(data_product=product) DESC,(purpose=consumer) DESC,version DESC LIMIT 1;
$$;
CREATE OR REPLACE FUNCTION knowledge_quality_policy(project text) RETURNS SETOF data_quality_policies LANGUAGE sql STABLE AS $$
    SELECT * FROM knowledge_quality_policy(project,'software_knowledge','development_guidance');
$$;

CREATE FUNCTION knowledge_quality_reason(item_id uuid,consumer text) RETURNS text LANGUAGE sql STABLE AS $$
    SELECT CASE
      WHEN k.status<>'approved' THEN 'not_approved'
      WHEN consumer NOT IN ('development_guidance','code_change','review') OR p.version IS NULL THEN 'policy_unavailable'
      WHEN v.id IS NULL THEN 'validation_required'
      WHEN v.candidate_version<>k.version OR v.project_id<>k.project_id
        OR v.content_sha256<>encode(digest(k.content,'sha256'),'hex') THEN 'version_conflict'
      WHEN v.verdict<>'pass' OR v.completed_at>now() OR (p.required_method='workpacket' AND v.method<>'workpacket') THEN 'validation_required'
      WHEN v.valid_until<=now() OR v.completed_at + CASE WHEN v.method='workpacket' OR consumer='code_change' THEN p.patch_max_age ELSE p.content_max_age END<=now() THEN 'validation_expired'
      WHEN s.validation_id IS NULL OR s.manifest_sha256<>v.source_manifest_sha256 THEN 'source_unverified'
      WHEN NOT s.valid THEN s.reason
      WHEN s.verified_at>now() OR s.verified_at+p.source_max_age<=now() THEN 'source_expired'
      WHEN octet_length(k.content)>p.max_content_bytes THEN 'content_too_large'
      WHEN length(trim(k.content))=0 OR length(trim(k.summary))=0 OR cardinality(k.procedure)=0 THEN 'content_incomplete'
      WHEN EXISTS(SELECT 1 FROM knowledge_items other WHERE other.project_id=k.project_id AND other.status='approved'
        AND other.approval_validation_id IS NOT NULL AND other.content=k.content AND (other.created_at,other.id)<(k.created_at,k.id)) THEN 'duplicate_content'
      ELSE 'eligible' END
    FROM knowledge_items k LEFT JOIN knowledge_validations v ON v.id=k.approval_validation_id
    LEFT JOIN knowledge_sources s ON s.validation_id=v.id
    LEFT JOIN LATERAL knowledge_quality_policy(k.project_id,'software_knowledge',consumer) p ON true WHERE k.id=item_id;
$$;
CREATE OR REPLACE FUNCTION knowledge_quality_reason(item_id uuid) RETURNS text LANGUAGE sql STABLE AS $$
    SELECT knowledge_quality_reason(item_id,'development_guidance');
$$;
CREATE FUNCTION knowledge_eligible(item_id uuid,consumer text) RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT COALESCE(knowledge_quality_reason(item_id,consumer)='eligible',false);
$$;

CREATE INDEX knowledge_duplicate_lookup_idx ON knowledge_items(project_id,(encode(digest(content,'sha256'),'hex'))) WHERE status='approved';
CREATE FUNCTION knowledge_duplicate_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status='approved' AND (TG_OP='INSERT' OR OLD.status<>'approved') THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(NEW.project_id || ':' || encode(digest(NEW.content,'sha256'),'hex'),0));
        IF EXISTS(SELECT 1 FROM knowledge_items k WHERE k.project_id=NEW.project_id AND k.id<>NEW.id
          AND k.status='approved' AND k.approval_validation_id IS NOT NULL AND k.content=NEW.content) THEN
            RAISE EXCEPTION 'quality_blocked: duplicate_content; reuse or revalidate the canonical candidate';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER knowledge_duplicate_guard BEFORE INSERT OR UPDATE ON knowledge_items FOR EACH ROW EXECUTE FUNCTION knowledge_duplicate_guard();

ALTER TABLE knowledge_projection_checks ADD COLUMN manifest jsonb;
ALTER TABLE quality_review_cases ADD COLUMN owner text NOT NULL DEFAULT 'platform-maintainers';
ALTER TABLE quality_review_cases ADD COLUMN recovery text NOT NULL DEFAULT 'Review source applicability, revalidate with human approval, then reindex';
CREATE TABLE outbox_retry_decisions (
    id uuid PRIMARY KEY,
    event_id bigint NOT NULL REFERENCES outbox_events(id),
    replacement_event_id bigint NOT NULL REFERENCES outbox_events(id),
    actor text NOT NULL REFERENCES principals(id),
    reason text NOT NULL,
    idempotency_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(event_id,idempotency_key)
);
CREATE TRIGGER outbox_retry_immutable BEFORE UPDATE OR DELETE ON outbox_retry_decisions FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
