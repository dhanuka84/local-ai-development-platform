ALTER TABLE principal_role_bindings DROP CONSTRAINT principal_role_bindings_role_check;
ALTER TABLE principal_role_bindings ADD CONSTRAINT principal_role_bindings_role_check CHECK(role IN(
 'controller','development','qa','product_owner','operations','cloud_reviewer','maintenance_executor','repository_analyzer','validation_executor','incident_diagnosis',
 'sdlc_builder','sdlc_evaluator','sdlc_delivery','sdlc_diagnosis','sdlc_remediation'));

CREATE TABLE sdlc_executions (
 id uuid PRIMARY KEY,
 project_id text NOT NULL REFERENCES projects(id),
 product_id text NOT NULL,
 owner text NOT NULL REFERENCES principals(id),
 owner_credential_id uuid NOT NULL REFERENCES principal_credentials(id),
 idempotency_key text NOT NULL,
 request_sha256 text NOT NULL,
 record jsonb NOT NULL,
 UNIQUE(project_id,owner,idempotency_key)
);
CREATE TABLE sdlc_execution_steps (
 id uuid PRIMARY KEY,
 run_id uuid NOT NULL REFERENCES sdlc_executions(id),
 step_number integer NOT NULL CHECK(step_number>0),
 actor text NOT NULL REFERENCES principals(id),
 lease_sha256 text NOT NULL,
 lease_until timestamptz NOT NULL,
 fence integer NOT NULL CHECK(fence>0),
 resource_key text NOT NULL,
 concurrency integer NOT NULL CHECK(concurrency>0),
 record jsonb NOT NULL,
 completed_at timestamptz,
 UNIQUE(run_id,step_number)
);
CREATE INDEX sdlc_steps_leases ON sdlc_execution_steps(resource_key,lease_until) WHERE completed_at IS NULL;
CREATE TABLE sdlc_execution_events (
 id uuid PRIMARY KEY,
 run_id uuid NOT NULL REFERENCES sdlc_executions(id),
 record jsonb NOT NULL
);
CREATE TRIGGER sdlc_event_immutable BEFORE UPDATE OR DELETE ON sdlc_execution_events
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TABLE sdlc_execution_results (
 step_id uuid PRIMARY KEY REFERENCES sdlc_execution_steps(id),
 actor text NOT NULL REFERENCES principals(id),
 sha256 text NOT NULL REFERENCES artifacts(sha256),
 result jsonb NOT NULL
);
CREATE TRIGGER sdlc_result_immutable BEFORE UPDATE OR DELETE ON sdlc_execution_results
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TABLE sdlc_execution_artifacts (
 run_id uuid NOT NULL REFERENCES sdlc_executions(id),
 step_id uuid NOT NULL REFERENCES sdlc_execution_steps(id),
 sha256 text NOT NULL REFERENCES artifacts(sha256),
 actor text NOT NULL REFERENCES principals(id),
 PRIMARY KEY(run_id,step_id,sha256)
);
CREATE TRIGGER sdlc_artifact_immutable BEFORE UPDATE OR DELETE ON sdlc_execution_artifacts
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TABLE sdlc_source_reservations (
 step_id uuid NOT NULL REFERENCES sdlc_execution_steps(id),
 idempotency_key text NOT NULL,
 query_sha256 text NOT NULL,
 PRIMARY KEY(step_id,idempotency_key)
);
CREATE TRIGGER sdlc_source_reservation_immutable BEFORE UPDATE OR DELETE ON sdlc_source_reservations
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE INDEX operation_execution ON operation_records(project_id,(record->>'execution_id'));
