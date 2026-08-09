ALTER TABLE review_records
    ADD COLUMN IF NOT EXISTS review_artifact_sha256 char(64) REFERENCES artifacts(sha256),
    ADD COLUMN IF NOT EXISTS context_manifest_artifact_sha256 char(64) REFERENCES artifacts(sha256);

CREATE INDEX IF NOT EXISTS review_records_artifact_idx
    ON review_records(review_artifact_sha256)
    WHERE review_artifact_sha256 IS NOT NULL;
