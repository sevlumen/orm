package entity

import (
	"testing"

	"github.com/sevlumen/orm/schema"
)

type foreignKeyUser struct {
	TenantID string `orm:"type:uuid"`
	ID       string `orm:"type:uuid"`
}

func (foreignKeyUser) TableName() string { return "users" }

func (foreignKeyUser) ConfigureORM(table *TableBuilder) {
	table.PrimaryKey("pk_users", "TenantID", "ID")
}

type foreignKeyPost struct {
	TenantID string `orm:"type:uuid"`
	UserID   string `orm:"column:user_id;type:uuid"`
}

func (foreignKeyPost) TableName() string { return "posts" }

func (foreignKeyPost) ConfigureORM(table *TableBuilder) {
	table.ForeignKey("fk_posts_user", "TenantID", "UserID").
		References("users", "tenant_id", "id").
		OnDelete(schema.Cascade).
		OnUpdate(schema.Restrict).
		InitiallyDeferred()
}

func TestConfigurerBuildsForeignKeyMetadata(t *testing.T) {
	t.Parallel()

	model, err := Parse(foreignKeyUser{}, foreignKeyPost{})
	if err != nil {
		t.Fatal(err)
	}
	foreignKeys := model.Tables[1].ForeignKeys
	if len(foreignKeys) != 1 {
		t.Fatalf("foreign keys = %#v", foreignKeys)
	}
	foreignKey := foreignKeys[0]
	if foreignKey.Name != "fk_posts_user" || foreignKey.ReferencedTable != "users" {
		t.Fatalf("unexpected foreign key: %#v", foreignKey)
	}
	if len(foreignKey.Columns) != 2 || foreignKey.Columns[1] != "user_id" {
		t.Fatalf("unexpected local columns: %v", foreignKey.Columns)
	}
	if foreignKey.OnDelete != schema.Cascade || foreignKey.OnUpdate != schema.Restrict || !foreignKey.Deferrable || !foreignKey.InitiallyDeferred {
		t.Fatalf("unexpected options: %#v", foreignKey)
	}
}

type incompleteForeignKey struct {
	UserID string `orm:"type:uuid"`
}

func (incompleteForeignKey) ConfigureORM(table *TableBuilder) {
	table.ForeignKey("fk_incomplete_user", "UserID")
}

func TestConfigurerRejectsIncompleteForeignKey(t *testing.T) {
	t.Parallel()
	if _, err := Parse(incompleteForeignKey{}); err == nil {
		t.Fatal("expected incomplete foreign key error")
	}
}
