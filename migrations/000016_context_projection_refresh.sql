ALTER TABLE context_definitions ADD COLUMN next_projection_check_at timestamptz NOT NULL DEFAULT now();
