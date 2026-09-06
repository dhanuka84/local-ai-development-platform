ALTER TABLE principal_role_bindings DROP CONSTRAINT principal_role_bindings_role_check;
ALTER TABLE principal_role_bindings ADD CONSTRAINT principal_role_bindings_role_check CHECK(role IN(
 'controller','development','qa','product_owner','operations','cloud_reviewer','maintenance_executor','repository_analyzer','validation_executor'));
