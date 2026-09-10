CREATE TABLE task_delegations (
    id uuid PRIMARY KEY,
    principal_id text NOT NULL UNIQUE REFERENCES principals(id),
    delegated_by text NOT NULL REFERENCES principals(id),
    -- Keep attribution after bootstrap credential rotation/deletion. Absence of
    -- this exact parent credential immediately invalidates the delegation.
    parent_credential_id uuid NOT NULL,
    project_id text NOT NULL REFERENCES projects(id),
    workflow_id uuid NOT NULL REFERENCES workflow_runs(id),
    task_id uuid NOT NULL REFERENCES workflow_task_checkpoints(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '1 hour')
);
CREATE INDEX task_delegations_task_idx ON task_delegations(task_id);

CREATE FUNCTION protect_task_delegation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'delegation attribution is immutable';
    END IF;
    IF (to_jsonb(NEW) - 'revoked_at') IS DISTINCT FROM (to_jsonb(OLD) - 'revoked_at')
       OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
        RAISE EXCEPTION 'only first revocation may change a delegation';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER task_delegation_immutable BEFORE UPDATE OR DELETE ON task_delegations
FOR EACH ROW EXECUTE FUNCTION protect_task_delegation();
