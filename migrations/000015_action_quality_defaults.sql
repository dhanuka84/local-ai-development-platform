ALTER TABLE context_definitions ADD COLUMN IF NOT EXISTS projection_model text NOT NULL DEFAULT '';
ALTER TABLE context_definitions ADD COLUMN IF NOT EXISTS projection_dimension integer NOT NULL DEFAULT 0;
INSERT INTO data_quality_policies(project_id,data_product,purpose,version,owner,source_max_age,content_max_age,patch_max_age,projection_max_age,required_method)
VALUES('*','software_knowledge','code_change',1,'platform-quality',interval '1 day',interval '1 day',interval '1 day',interval '1 day','workpacket');
