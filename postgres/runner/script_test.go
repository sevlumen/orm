package runner

import "testing"

func TestValidateMigrationScript(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"CREATE TABLE users (id bigint); INSERT INTO users VALUES (1);",
		"DO $$ BEGIN RAISE NOTICE 'COMMIT'; END $$;",
		"SELECT 'ROLLBACK'; -- COMMIT\nALTER TABLE users ADD COLUMN name text;",
		"/* BEGIN; /* nested */ COMMIT; */ SELECT 1;",
		`SELECT "COMMIT" FROM users;`,
		`SELECT E'escaped\'quote; COMMIT'; SELECT 1;`,
	}
	for _, script := range allowed {
		if err := validateMigrationScript(script); err != nil {
			t.Fatalf("allowed script rejected: %q: %v", script, err)
		}
	}

	prohibited := []string{
		"BEGIN; CREATE TABLE users(id int); COMMIT;",
		"START TRANSACTION; SELECT 1;",
		"ROLLBACK;",
		"ABORT;",
		"SAVEPOINT before_change;",
		"RELEASE SAVEPOINT before_change;",
		"PREPARE TRANSACTION 'migration';",
	}
	for _, script := range prohibited {
		if err := validateMigrationScript(script); err == nil {
			t.Fatalf("prohibited script accepted: %q", script)
		}
	}
}

func TestValidateMigrationScriptRejectsMalformedLexicalInput(t *testing.T) {
	t.Parallel()
	values := []string{"SELECT 'unterminated", `SELECT "unterminated`, "/* unterminated", "$tag$unterminated"}
	for _, value := range values {
		if err := validateMigrationScript(value); err == nil {
			t.Fatalf("malformed script accepted: %q", value)
		}
	}
}
