package postgres

import (
	"strings"
	"testing"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

func TestRenderCreateSchemaWithConstraintsAndIndexes(t *testing.T) {
	t.Parallel()
	model := schema.Schema{Tables: []schema.Table{{
		Name: "memberships",
		Columns: []schema.Column{
			{Name: "tenant_id", Type: "uuid"},
			{Name: "user_id", Type: "uuid"},
			{Name: "email", Type: "text"},
			{Name: "deleted_at", Type: "timestamptz", Nullable: true},
		},
		PrimaryKey:        &schema.PrimaryKey{Name: "pk_memberships", Columns: []string{"tenant_id", "user_id"}},
		UniqueConstraints: []schema.UniqueConstraint{{Name: "uq_memberships_email", Columns: []string{"tenant_id", "email"}}},
		Checks:            []schema.CheckConstraint{{Name: "ck_memberships_email", Expression: "length(email) > 3"}},
		Indexes: []schema.Index{
			{Name: "ix_memberships_email", Columns: []string{"email"}, Include: []string{"user_id"}, Predicate: "deleted_at IS NULL"},
			{Name: "ix_memberships_lower_email", Expression: "lower(email)", Unique: true, Method: "btree"},
		},
	}}}
	got, err := RenderCreateSchema(model)
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE TABLE \"memberships\" (\n" +
		"    \"tenant_id\" uuid NOT NULL,\n" +
		"    \"user_id\" uuid NOT NULL,\n" +
		"    \"email\" text NOT NULL,\n" +
		"    \"deleted_at\" timestamptz,\n" +
		"    CONSTRAINT \"pk_memberships\" PRIMARY KEY (\"tenant_id\", \"user_id\"),\n" +
		"    CONSTRAINT \"uq_memberships_email\" UNIQUE (\"tenant_id\", \"email\"),\n" +
		"    CONSTRAINT \"ck_memberships_email\" CHECK (length(email) > 3)\n" +
		");\n" +
		"CREATE INDEX \"ix_memberships_email\" ON \"memberships\" (\"email\") INCLUDE (\"user_id\") WHERE deleted_at IS NULL;\n" +
		"CREATE UNIQUE INDEX \"ix_memberships_lower_email\" ON \"memberships\" USING btree ((lower(email)));\n"
	if got != want {
		t.Fatalf("unexpected SQL\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestRenderMetadataMigrationIsReversible(t *testing.T) {
	t.Parallel()
	before, err := migration.NewSnapshot(schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "email", Type: "text"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := migration.NewSnapshot(schema.Schema{Tables: []schema.Table{{
		Name:              "users",
		Columns:           []schema.Column{{Name: "id", Type: "uuid", PrimaryKey: true}, {Name: "email", Type: "text"}},
		UniqueConstraints: []schema.UniqueConstraint{{Name: "uq_users_email", Columns: []string{"email"}}},
		Checks:            []schema.CheckConstraint{{Name: "ck_users_email", Expression: "length(email) > 3"}},
		Indexes:           []schema.Index{{Name: "ix_users_lower_email", Expression: "lower(email)"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := migration.Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := RenderMigration(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`CREATE INDEX "ix_users_lower_email" ON "users" ((lower(email)));`,
		`ALTER TABLE "users" ADD CONSTRAINT "uq_users_email" UNIQUE ("email");`,
		`ALTER TABLE "users" ADD CONSTRAINT "ck_users_email" CHECK (length(email) > 3);`,
	} {
		if !strings.Contains(generated.Up, fragment) {
			t.Fatalf("up migration missing %q:\n%s", fragment, generated.Up)
		}
	}
	for _, fragment := range []string{
		`DROP INDEX "ix_users_lower_email";`,
		`ALTER TABLE "users" DROP CONSTRAINT "uq_users_email";`,
		`ALTER TABLE "users" DROP CONSTRAINT "ck_users_email";`,
	} {
		if !strings.Contains(generated.Down, fragment) {
			t.Fatalf("down migration missing %q:\n%s", fragment, generated.Down)
		}
	}
}
