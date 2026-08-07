package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sevlumen/orm/schema"
)

func TestGeneratedConstraintAndIndexSQLAgainstPostgreSQL(t *testing.T) {
	database := openIntegrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	tableName := "sl_meta_" + suffix
	primaryName := "pk_" + tableName
	uniqueName := "uq_" + tableName + "_email"
	checkName := "ck_" + tableName + "_email"
	partialIndex := "ix_" + tableName + "_active"
	expressionIndex := "ix_" + tableName + "_lower_email"

	model := schema.Schema{Tables: []schema.Table{{
		Name: tableName,
		Columns: []schema.Column{
			{Name: "tenant_id", Type: "bigint"},
			{Name: "user_id", Type: "bigint"},
			{Name: "email", Type: "text"},
			{Name: "deleted_at", Type: "timestamptz", Nullable: true},
		},
		PrimaryKey:        &schema.PrimaryKey{Name: primaryName, Columns: []string{"tenant_id", "user_id"}},
		UniqueConstraints: []schema.UniqueConstraint{{Name: uniqueName, Columns: []string{"tenant_id", "email"}}},
		Checks:            []schema.CheckConstraint{{Name: checkName, Expression: "length(email) > 3"}},
		Indexes: []schema.Index{
			{Name: partialIndex, Columns: []string{"email"}, Include: []string{"user_id"}, Predicate: "deleted_at IS NULL"},
			{Name: expressionIndex, Expression: "lower(email)", Unique: true},
		},
	}}}
	ddl, err := RenderCreateSchema(model)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, cleanupErr := database.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+quoteIdentifier(tableName)); cleanupErr != nil {
			t.Logf("cleanup failed: %v", cleanupErr)
		}
	}()

	executeIntegrationScript(t, ctx, database, ddl)

	qualified := quoteIdentifier(tableName)
	if _, err := database.ExecContext(ctx, "INSERT INTO "+qualified+" (tenant_id, user_id, email) VALUES (1, 1, 'member@example.com')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO "+qualified+" (tenant_id, user_id, email) VALUES (1, 2, 'member@example.com')"); err == nil {
		t.Fatal("expected unique constraint violation")
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO "+qualified+" (tenant_id, user_id, email) VALUES (2, 1, 'x')"); err == nil {
		t.Fatal("expected check constraint violation")
	}

	var constraints int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid = to_regclass($1) AND conname IN ($2, $3, $4)`, tableName, primaryName, uniqueName, checkName).Scan(&constraints); err != nil {
		t.Fatal(err)
	}
	if constraints != 3 {
		t.Fatalf("constraint count = %d, want 3", constraints)
	}
	var indexes int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = $1 AND indexname IN ($2, $3)`, tableName, partialIndex, expressionIndex).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 2 {
		t.Fatalf("index count = %d, want 2", indexes)
	}
}
