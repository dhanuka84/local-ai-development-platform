CREATE TABLE operation_records (
    id uuid PRIMARY KEY,
    operation_id uuid NOT NULL,
    project_id text NOT NULL,
    workflow_id text NOT NULL DEFAULT '',
    task_id text NOT NULL DEFAULT '',
    trace_id char(32) NOT NULL CHECK(trace_id ~ '^[a-f0-9]{32}$'),
    span_id char(16) NOT NULL CHECK(span_id ~ '^[a-f0-9]{16}$'),
    phase text NOT NULL CHECK(phase IN ('intent','outcome','commit','reconciliation')),
    outcome text NOT NULL CHECK(outcome IN ('pending','success','denied','failed','unknown')),
    record jsonb NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(operation_id,phase)
);
CREATE INDEX operation_workflow_idx ON operation_records(project_id,workflow_id,recorded_at,id);
CREATE TRIGGER operation_record_immutable BEFORE UPDATE OR DELETE ON operation_records
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TABLE trace_export_queue (
    record_id uuid PRIMARY KEY REFERENCES operation_records(id),
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    exported_at timestamptz
);
