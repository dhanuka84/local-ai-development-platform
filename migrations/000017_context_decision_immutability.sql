CREATE FUNCTION context_decision_fields_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status<>'pending' AND (NEW.status,NEW.approved_by,NEW.approved_at)
      IS DISTINCT FROM (OLD.status,OLD.approved_by,OLD.approved_at) THEN
        RAISE EXCEPTION 'version_conflict: decided context history is immutable; publish a new version';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER context_decision_fields_guard BEFORE UPDATE ON context_definitions
FOR EACH ROW EXECUTE FUNCTION context_decision_fields_guard();
