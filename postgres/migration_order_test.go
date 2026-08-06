package postgres

import (
	"strings"
	"testing"

	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/schema"
)

func TestRenderMigrationDropsRelationsBeforeReusingNames(t *testing.T) {
	t.Parallel()

	oldTable := schema.Table{
		Name:    "old_users",
		Columns: []schema.Column{{Name: "email", Type: "text"}},
		Indexes: []schema.Index{{Name: "ix_shared_email", Columns: []string{"email"}}},
	}
	newTable := schema.Table{
		Name:    "new_users",
		Columns: []schema.Column{{Name: "email", Type: "text"}},
		Indexes: []schema.Index{{Name: "ix_shared_email", Columns: []string{"email"}}},
	}

	result, err := RenderMigration(migration.Plan{Operations: []migration.Operation{
		{Kind: migration.CreateTable, Table: newTable.Name, AfterTable: &newTable},
		{Kind: migration.DropTable, Table: oldTable.Name, BeforeTable: &oldTable, Risk: migration.RiskDestructive},
	}})
	if err != nil {
		t.Fatal(err)
	}

	upDrop := strings.Index(result.Up, `DROP TABLE "old_users";`)
	upCreate := strings.Index(result.Up, `CREATE TABLE "new_users"`)
	if upDrop < 0 || upCreate < 0 || upDrop > upCreate {
		t.Fatalf("up SQL does not free relation names before reuse:\n%s", result.Up)
	}

	downDrop := strings.Index(result.Down, `DROP TABLE "new_users";`)
	downCreate := strings.Index(result.Down, `CREATE TABLE "old_users"`)
	if downDrop < 0 || downCreate < 0 || downDrop > downCreate {
		t.Fatalf("down SQL does not free relation names before reuse:\n%s", result.Down)
	}
}
