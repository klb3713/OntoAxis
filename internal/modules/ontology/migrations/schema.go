// Package migrations owns the idempotent DDL for the ontology schema. The
// statements are executed through internal/platform/db.ExecStatements by the
// module lifecycle, so each slice entry is a single SQL statement.
package migrations

// Statements returns the forward, idempotent DDL for the ontology schema.
func Statements() []string {
	return []string{
		`CREATE SCHEMA IF NOT EXISTS ontology`,

		`CREATE TABLE IF NOT EXISTS ontology.object_types (
			id uuid PRIMARY KEY,
			tenant_id uuid NOT NULL,
			name text NOT NULL,
			display_name text NOT NULL DEFAULT '',
			description text NOT NULL DEFAULT '',
			primary_key_property text NOT NULL DEFAULT '',
			backing_dataset_id uuid NULL,
			dataset_sync_mode text NULL,
			status text NOT NULL DEFAULT 'active',
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			deleted_at timestamptz NULL,
			CONSTRAINT object_types_status_check CHECK (status IN ('active', 'draft', 'deprecated')),
			CONSTRAINT object_types_tenant_name_key UNIQUE (tenant_id, name)
		)`,

		`CREATE TABLE IF NOT EXISTS ontology.properties (
			id uuid PRIMARY KEY,
			tenant_id uuid NOT NULL,
			object_type_id uuid NOT NULL,
			name text NOT NULL,
			display_name text NOT NULL DEFAULT '',
			description text NOT NULL DEFAULT '',
			property_type text NOT NULL,
			required boolean NOT NULL DEFAULT false,
			unique_constraint boolean NOT NULL DEFAULT false,
			config jsonb NOT NULL DEFAULT '{}'::jsonb,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			deleted_at timestamptz NULL,
			CONSTRAINT properties_object_type_fk FOREIGN KEY (object_type_id) REFERENCES ontology.object_types (id),
			CONSTRAINT properties_tenant_object_name_key UNIQUE (tenant_id, object_type_id, name)
		)`,

		`CREATE TABLE IF NOT EXISTS ontology.link_types (
			id uuid PRIMARY KEY,
			tenant_id uuid NOT NULL,
			name text NOT NULL,
			display_name text NOT NULL DEFAULT '',
			description text NOT NULL DEFAULT '',
			source_type_id uuid NOT NULL,
			target_type_id uuid NOT NULL,
			cardinality text NOT NULL DEFAULT 'many_to_many',
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			deleted_at timestamptz NULL,
			CONSTRAINT link_types_cardinality_check CHECK (cardinality IN ('one_to_one', 'one_to_many', 'many_to_many')),
			CONSTRAINT link_types_tenant_name_key UNIQUE (tenant_id, name),
			CONSTRAINT link_types_source_type_fk FOREIGN KEY (source_type_id) REFERENCES ontology.object_types (id),
			CONSTRAINT link_types_target_type_fk FOREIGN KEY (target_type_id) REFERENCES ontology.object_types (id)
		)`,

		`CREATE INDEX IF NOT EXISTS object_types_tenant_deleted_idx ON ontology.object_types (tenant_id, deleted_at)`,
		`CREATE INDEX IF NOT EXISTS properties_tenant_object_type_deleted_idx ON ontology.properties (tenant_id, object_type_id, deleted_at)`,
		`CREATE INDEX IF NOT EXISTS link_types_tenant_deleted_idx ON ontology.link_types (tenant_id, deleted_at)`,
		`CREATE INDEX IF NOT EXISTS link_types_source_type_idx ON ontology.link_types (tenant_id, source_type_id)`,
		`CREATE INDEX IF NOT EXISTS link_types_target_type_idx ON ontology.link_types (tenant_id, target_type_id)`,

		`ALTER TABLE ontology.object_types ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ontology.object_types FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS object_types_tenant_isolation ON ontology.object_types`,
		`CREATE POLICY object_types_tenant_isolation ON ontology.object_types
			USING (tenant_id = current_setting('app.tenant_id', true)::uuid OR current_setting('app.is_platform_admin', true) = 'true')
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid OR current_setting('app.is_platform_admin', true) = 'true')`,

		`ALTER TABLE ontology.properties ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ontology.properties FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS properties_tenant_isolation ON ontology.properties`,
		`CREATE POLICY properties_tenant_isolation ON ontology.properties
			USING (tenant_id = current_setting('app.tenant_id', true)::uuid OR current_setting('app.is_platform_admin', true) = 'true')
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid OR current_setting('app.is_platform_admin', true) = 'true')`,

		`ALTER TABLE ontology.link_types ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ontology.link_types FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS link_types_tenant_isolation ON ontology.link_types`,
		`CREATE POLICY link_types_tenant_isolation ON ontology.link_types
			USING (tenant_id = current_setting('app.tenant_id', true)::uuid OR current_setting('app.is_platform_admin', true) = 'true')
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid OR current_setting('app.is_platform_admin', true) = 'true')`,
	}
}

// DownStatements returns the rollback DDL. It is intentionally explicit and
// destructive: dropping the schema removes only ontology-owned objects.
func DownStatements() []string {
	return []string{
		`DROP SCHEMA IF EXISTS ontology CASCADE`,
	}
}
