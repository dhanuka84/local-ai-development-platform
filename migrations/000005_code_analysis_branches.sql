ALTER TABLE code_analysis_runs
    ADD COLUMN IF NOT EXISTS branch text NOT NULL DEFAULT '';
