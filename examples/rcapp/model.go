// Package rcapp is the maintained release-candidate application used to
// exercise Sevlumen ORM's complete v1 workflow against PostgreSQL.
package rcapp

import (
	"github.com/sevlumen/orm/entity"
	"github.com/sevlumen/orm/schema"
)

// Account is the final v1 application account model.
type Account struct {
	ID          int64   `orm:"column:id;type:bigint;primaryKey;insertOnly"`
	LoginEmail  string  `orm:"column:login_email;type:text;notNull;unique"`
	DisplayName *string `orm:"column:display_name;type:text;nullable"`
	Active      bool    `orm:"column:active;type:boolean;notNull;default:true"`
}

func (Account) TableName() string { return "accounts" }

func (Account) ConfigureORM(builder *entity.TableBuilder) {
	builder.Index("ix_accounts_active_login", "Active", "LoginEmail").Include("DisplayName")
}

// Order is the final v1 order model.
type Order struct {
	ID        int64  `orm:"column:id;type:bigint;primaryKey;insertOnly"`
	AccountID int64  `orm:"column:account_id;type:bigint;notNull"`
	Total     int64  `orm:"column:total;type:bigint;notNull"`
	Status    string `orm:"column:status;type:text;notNull;default:'pending'"`
}

func (Order) TableName() string { return "orders" }

func (Order) ConfigureORM(builder *entity.TableBuilder) {
	builder.ForeignKey("fk_orders_accounts", "AccountID").
		References("accounts", "id").
		OnDelete(schema.Cascade)
	builder.Index("ix_orders_account_id", "AccountID")
	builder.Check("ck_orders_total_nonnegative", `"total" >= 0`)
}
