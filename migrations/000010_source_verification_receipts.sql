-- Attempts are append-only; the successful verification clock does not move
-- when the source is unavailable or changed.
CREATE TABLE knowledge_source_check_receipts (
    id bigserial PRIMARY KEY,
    validation_id uuid NOT NULL REFERENCES knowledge_validations(id),
    checked_at timestamptz NOT NULL DEFAULT now(),
    valid boolean NOT NULL,
    reason text NOT NULL
);
CREATE INDEX knowledge_source_checks_latest_idx ON knowledge_source_check_receipts(validation_id,checked_at DESC);
CREATE TRIGGER knowledge_source_check_immutable BEFORE UPDATE OR DELETE ON knowledge_source_check_receipts
FOR EACH ROW EXECUTE FUNCTION knowledge_evidence_immutable();
