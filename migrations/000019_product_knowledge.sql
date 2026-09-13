-- Immutable versioned product context. Proposed/generated content has no
-- retrieval head until an exact, validated, accountable publication decision.
ALTER TABLE principal_role_bindings DROP CONSTRAINT principal_role_bindings_role_check;
ALTER TABLE principal_role_bindings ADD CONSTRAINT principal_role_bindings_role_check CHECK(role IN(
 'controller','development','qa','product_owner','operations','cloud_reviewer','maintenance_executor','repository_analyzer','validation_executor','incident_diagnosis'));
CREATE TABLE product_records (
    id uuid PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id),
    product_id text NOT NULL,
    record_key text NOT NULL,
    version integer NOT NULL CHECK(version > 0),
    kind text NOT NULL CHECK(kind IN ('product','brs','feature','code','test','release','incident','hypothesis','procedure','observation','intent')),
    origin text NOT NULL CHECK(origin IN ('proposal','source_adapter')),
    sha256 text NOT NULL CHECK(sha256 ~ '^[a-f0-9]{64}$'),
    record jsonb NOT NULL,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(project_id,product_id,record_key,version),
    UNIQUE(project_id,id)
);
CREATE TRIGGER product_record_immutable BEFORE UPDATE OR DELETE ON product_records
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TABLE product_record_validations (
    id uuid PRIMARY KEY,
    project_id text NOT NULL,
    record_id uuid NOT NULL,
    sha256 text NOT NULL,
    actor text NOT NULL REFERENCES principals(id),
    method text NOT NULL CHECK(method IN ('human_attestation','source_adapter')),
    evidence_sha256 text NOT NULL REFERENCES artifacts(sha256),
    valid_until timestamptz NOT NULL,
    FOREIGN KEY(project_id,record_id) REFERENCES product_records(project_id,id)
);
CREATE TRIGGER product_validation_immutable BEFORE UPDATE OR DELETE ON product_record_validations
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TABLE product_record_decisions (
    record_id uuid PRIMARY KEY REFERENCES product_records(id),
    decision text NOT NULL CHECK(decision IN ('accept','reject')),
    actor text NOT NULL REFERENCES principals(id),
    validation_id uuid REFERENCES product_record_validations(id),
    reason text NOT NULL CHECK(length(reason)>0),
    idempotency_key text NOT NULL CHECK(length(idempotency_key)>0),
    request_sha256 text NOT NULL,
    decided_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER product_decision_immutable BEFORE UPDATE OR DELETE ON product_record_decisions
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TABLE product_record_heads (
    project_id text NOT NULL,
    product_id text NOT NULL,
    record_key text NOT NULL,
    record_id uuid NOT NULL REFERENCES product_records(id),
    PRIMARY KEY(project_id,product_id,record_key)
);
CREATE TABLE product_relations (
    id uuid PRIMARY KEY,
    project_id text NOT NULL,
    product_id text NOT NULL,
    from_id uuid NOT NULL,
    to_id uuid NOT NULL,
    kind text NOT NULL CHECK(kind IN ('contains','specifies','implements','tests','deployed_as','observed_in','affects','supports','contradicts','supersedes')),
    evidence text NOT NULL CHECK(length(evidence)>0),
    actor text NOT NULL REFERENCES principals(id),
    CHECK(from_id<>to_id),
    FOREIGN KEY(project_id,from_id) REFERENCES product_records(project_id,id),
    FOREIGN KEY(project_id,to_id) REFERENCES product_records(project_id,id),
    UNIQUE(project_id,from_id,to_id,kind)
);
CREATE TRIGGER product_relation_immutable BEFORE UPDATE OR DELETE ON product_relations
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE INDEX product_relation_to ON product_relations(project_id,to_id);
CREATE TABLE product_projections (
    record_id uuid PRIMARY KEY REFERENCES product_records(id),
    sha256 text NOT NULL,
    model text NOT NULL,
    dimension integer NOT NULL CHECK(dimension>0),
    verified_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION product_record_eligible(record_uuid uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
      SELECT 1 FROM product_records r
      JOIN product_record_heads h ON h.record_id=r.id AND h.project_id=r.project_id AND h.product_id=r.product_id AND h.record_key=r.record_key
      WHERE r.id=record_uuid AND (r.expires_at IS NULL OR r.expires_at>now())
      AND ((r.origin='source_adapter' AND r.kind='observation') OR EXISTS (
        SELECT 1 FROM product_record_decisions d JOIN product_record_validations v ON v.id=d.validation_id
        WHERE d.record_id=r.id AND d.decision='accept' AND v.record_id=r.id AND v.sha256=r.sha256 AND v.valid_until>d.decided_at
      ))
    )
$$;

CREATE TABLE product_source_receipts (
    id uuid PRIMARY KEY,
    project_id text NOT NULL,
    source_id text NOT NULL,
    actor text NOT NULL REFERENCES principals(id),
    idempotency_key text NOT NULL,
    receipt jsonb NOT NULL,
    UNIQUE(project_id,source_id,actor,idempotency_key)
);
CREATE TRIGGER product_source_receipt_immutable BEFORE UPDATE OR DELETE ON product_source_receipts
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();

CREATE TABLE product_evaluations (
    id uuid PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects(id),
    actor text NOT NULL REFERENCES principals(id),
    request_sha256 text NOT NULL,
    evaluation jsonb NOT NULL,
    UNIQUE(project_id,actor,request_sha256)
);
CREATE TRIGGER product_evaluation_immutable BEFORE UPDATE OR DELETE ON product_evaluations
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
