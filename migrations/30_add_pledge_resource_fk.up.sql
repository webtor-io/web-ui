ALTER TABLE vault.pledge
	ADD CONSTRAINT pledge_resource_fk FOREIGN KEY (resource_id)
		REFERENCES vault.resource (resource_id) ON DELETE CASCADE;
