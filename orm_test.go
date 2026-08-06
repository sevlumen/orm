package orm_test

import (
	"testing"
	"time"

	orm "github.com/sevlumen/orm"
)

type User struct {
	ID          string     `orm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Email       string     `orm:"type:varchar(320);notNull;unique"`
	DisplayName *string    `orm:"column:display_name;type:varchar(200)"`
	Active      bool       `orm:"notNull;default:true"`
	CreatedAt   time.Time  `orm:"column:created_at;notNull;default:now()"`
	DeletedAt   *time.Time `orm:"column:deleted_at"`
}

func (User) TableName() string { return "users" }

func TestPostgreSQLSchema(t *testing.T) {
	t.Parallel()

	got, err := orm.PostgreSQLSchema(User{})
	if err != nil {
		t.Fatalf("PostgreSQLSchema() error = %v", err)
	}

	want := "CREATE TABLE \"users\" (\n" +
		"    \"id\" uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,\n" +
		"    \"email\" varchar(320) NOT NULL UNIQUE,\n" +
		"    \"display_name\" varchar(200),\n" +
		"    \"active\" boolean NOT NULL DEFAULT true,\n" +
		"    \"created_at\" timestamptz NOT NULL DEFAULT now(),\n" +
		"    \"deleted_at\" timestamptz\n" +
		");\n"

	if got != want {
		t.Fatalf("unexpected SQL\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestUnsupportedType(t *testing.T) {
	t.Parallel()

	type Invalid struct {
		Metadata map[int]string
	}

	if _, err := orm.PostgreSQLSchema(Invalid{}); err == nil {
		t.Fatal("expected unsupported type error")
	}
}
