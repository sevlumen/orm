package entity

import (
	"testing"

	"github.com/sevlumen/orm/schema"
)

type configuredMembership struct {
	TenantID string `orm:"type:uuid"`
	UserID   string `orm:"column:user_id;type:uuid"`
	Email    string
	Deleted  *string `orm:"column:deleted_at"`
}

func (configuredMembership) TableName() string { return "memberships" }

func (configuredMembership) ConfigureORM(table *TableBuilder) {
	table.PrimaryKey("pk_memberships", "TenantID", "UserID")
	table.Unique("uq_memberships_email", "TenantID", "Email")
	table.Check("ck_memberships_email", "length(email) > 3")
	table.Index("ix_memberships_email", "Email").Include("UserID").Where("deleted_at IS NULL")
	table.ExpressionIndex("ix_memberships_lower_email", "lower(email)").Unique().Using("btree")
}

func TestConfigurerBuildsTableMetadata(t *testing.T) {
	t.Parallel()
	model, err := Parse(configuredMembership{})
	if err != nil {
		t.Fatal(err)
	}
	table := model.Tables[0]
	if table.PrimaryKey == nil || len(table.PrimaryKey.Columns) != 2 || table.PrimaryKey.Columns[1] != "user_id" {
		t.Fatalf("unexpected primary key: %#v", table.PrimaryKey)
	}
	if len(table.UniqueConstraints) != 1 || len(table.Checks) != 1 || len(table.Indexes) != 2 {
		t.Fatalf("unexpected configuration: %#v", table)
	}
	if got := table.Indexes[0].Include; len(got) != 1 || got[0] != "user_id" {
		t.Fatalf("unexpected include columns: %v", got)
	}
}

func TestConfigurerRejectsUnknownField(t *testing.T) {
	t.Parallel()
	table := schema.Table{Name: "invalid", Columns: []schema.Column{{Name: "id", Type: "integer"}}}
	builder := &TableBuilder{table: &table, fields: map[string]string{"ID": "id", "id": "id"}}
	builder.Index("ix_invalid", "Missing")
	if builder.problem == nil {
		t.Fatal("expected unknown field error")
	}
}
