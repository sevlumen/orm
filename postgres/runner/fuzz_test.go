package runner

import (
	"strings"
	"testing"
)

func FuzzValidateMigrationScript(f *testing.F) {
	for _, seed := range []string{
		`CREATE TABLE users (id bigint);`,
		`BEGIN; CREATE TABLE users (id bigint);`,
		`START TRANSACTION; SELECT 1;`,
		`PREPARE TRANSACTION 'x';`,
		`RELEASE SAVEPOINT injected;`,
		`COPY users FROM STDIN;`,
		`SELECT 'BEGIN; DROP TABLE users;';`,
		`DO $$ BEGIN RAISE NOTICE 'safe'; END $$;`,
		`/* nested /* comment */ comment */ CREATE TABLE users (id bigint);`,
		`CREATE TABLE "BEGIN; DROP TABLE users;" (id bigint);`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, script string) {
		first := validateMigrationScript(script)
		second := validateMigrationScript(script)
		if (first == nil) != (second == nil) {
			t.Fatalf("migration validation is non-deterministic: first=%v second=%v", first, second)
		}
	})
}

func TestMigrationTransactionControlSeedCorpus(t *testing.T) {
	for _, script := range []string{
		"BEGIN;",
		"begin work;",
		"COMMIT;",
		"ROLLBACK;",
		"SAVEPOINT injected;",
		"RELEASE SAVEPOINT injected;",
		"START TRANSACTION;",
		"PREPARE TRANSACTION 'injected';",
		"COPY users FROM STDIN;",
	} {
		if err := validateMigrationScript(script); err == nil {
			t.Fatalf("accepted prohibited migration statement %q", script)
		}
	}

	for _, script := range []string{
		"SELECT 'BEGIN;';",
		"SELECT $$COMMIT;$$;",
		"CREATE TABLE \"ROLLBACK\" (id bigint);",
		"-- BEGIN\nSELECT 1;",
		"/* COMMIT */ SELECT 1;",
	} {
		if err := validateMigrationScript(script); err != nil {
			t.Fatalf("rejected transaction keywords in non-command context %q: %v", script, err)
		}
	}
}

func FuzzStatementPrefixesBounded(f *testing.F) {
	for _, seed := range []string{"", ";;;", "SELECT 1", strings.Repeat("/*", 20)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		prefixes, err := statementPrefixes(input, 2)
		if err != nil {
			return
		}
		for _, prefix := range prefixes {
			if len(prefix) > 2 {
				t.Fatalf("statement prefix exceeded requested bound: %#v", prefix)
			}
		}
	})
}
