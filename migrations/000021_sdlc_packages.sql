CREATE TABLE sdlc_package_evaluations (
 id uuid PRIMARY KEY, project_id text NOT NULL REFERENCES projects(id), package_id text NOT NULL,
 package_sha256 text NOT NULL, actor text NOT NULL REFERENCES principals(id), record jsonb NOT NULL
);
CREATE TRIGGER sdlc_package_evaluation_immutable BEFORE UPDATE OR DELETE ON sdlc_package_evaluations
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
CREATE TABLE sdlc_package_activations (
 project_id text NOT NULL REFERENCES projects(id), target_id text NOT NULL, role text NOT NULL,
 package_sha256 text NOT NULL, evaluation_id uuid NOT NULL REFERENCES sdlc_package_evaluations(id), record jsonb NOT NULL,
 PRIMARY KEY(project_id,target_id,role)
);
CREATE TABLE sdlc_package_decisions (
 id uuid PRIMARY KEY, project_id text NOT NULL REFERENCES projects(id), target_id text NOT NULL,
 role text NOT NULL, package_sha256 text NOT NULL, actor text NOT NULL REFERENCES principals(id), record jsonb NOT NULL
);
CREATE TRIGGER sdlc_package_decision_immutable BEFORE UPDATE OR DELETE ON sdlc_package_decisions
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
