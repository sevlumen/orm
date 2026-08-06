package orm_test

import (
	"strings"
	"testing"

	orm "github.com/sevlumen/orm"
	"github.com/sevlumen/orm/migration"
)

type legacyAccount struct {
	ID int64
}

func (legacyAccount) TableName() string { return "accounts" }

type renamedCustomer struct {
	CustomerID int64 `orm:"column:customer_id"`
}

func (renamedCustomer) TableName() string { return "customers" }

func TestPostgreSQLMigrationWithOptions(t *testing.T) {
	t.Parallel()

	before, err := orm.BuildSnapshot(legacyAccount{})
	if err != nil {
		t.Fatal(err)
	}
	generated, after, err := orm.PostgreSQLMigrationWithOptions(
		before,
		migration.DiffOptions{Renames: []migration.Rename{
			{Kind: migration.RenameTable, From: "accounts", To: "customers"},
			{Kind: migration.RenameColumn, Table: "customers", From: "id", To: "customer_id"},
		}},
		renamedCustomer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generated.Up, `ALTER TABLE "accounts" RENAME TO "customers";`) ||
		!strings.Contains(generated.Up, `ALTER TABLE "customers" RENAME COLUMN "id" TO "customer_id";`) {
		t.Fatalf("unexpected up SQL:\n%s", generated.Up)
	}
	if after.Schema.Tables[0].Name != "customers" || after.Schema.Tables[0].Columns[0].Name != "customer_id" {
		t.Fatalf("unexpected after snapshot: %#v", after)
	}
}
