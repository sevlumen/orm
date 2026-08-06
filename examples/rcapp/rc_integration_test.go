package rcapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sevlumen/orm/migration"
	"github.com/sevlumen/orm/postgres/query"
)

const (
	initialMigrationID     = "20260806010000_initial_schema"
	safeMigrationID        = "20260806020000_add_legacy_note"
	upgradeMigrationID     = "20260806030000_upgrade_accounts"
	destructiveMigrationID = "20260806040000_drop_legacy_note"
)

func TestReleaseCandidateWorkflow(t *testing.T) {
	databaseURL := os.Getenv("SEVLUMEN_TEST_DATABASE_URL")
	ormBinary := os.Getenv("SEVLUMEN_RC_ORM_BINARY")
	if databaseURL == "" || ormBinary == "" {
		t.Skip("set SEVLUMEN_TEST_DATABASE_URL and SEVLUMEN_RC_ORM_BINARY to run the RC application")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	resetDatabase(t, ctx, pool)

	runCLI(t, ctx, ormBinary, 0,
		"generate", "--dir", packageDirectory(t), "--output", "orm_gen.go",
		"--type", "Account", "--type", "Order", "--check",
	)

	workspace := t.TempDir()
	migrationsDirectory := filepath.Join(workspace, "migrations")
	initialPath := writeLoadedSnapshot(t, workspace, "initial.snapshot.json", InitialSnapshot)
	safePath := writeLoadedSnapshot(t, workspace, "safe.snapshot.json", SafeSnapshot)
	finalPath := writeLoadedSnapshot(t, workspace, "final.snapshot.json", FinalSnapshot)
	destructivePath := writeLoadedSnapshot(t, workspace, "destructive.snapshot.json", DestructiveSnapshot)
	renamesPath := writeRenames(t, workspace, UpgradeRenames())

	// Fresh database: create and apply the initial review-risk schema.
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", initialPath, "--id", initialMigrationID,
		"--migrations", migrationsDirectory, "--max-risk", "review",
	)
	runCLI(t, ctx, ormBinary, 0, "validate", "--migrations", migrationsDirectory)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "review",
	)

	mustExec(t, ctx, pool, `INSERT INTO users (id, email, active) VALUES (1, $1, true)`, "legacy@example.test")
	mustExec(t, ctx, pool, `INSERT INTO orders (id, user_id, total) VALUES (1, 1, 1200)`)

	// Safe migration on populated legacy data.
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", safePath, "--id", safeMigrationID,
		"--migrations", migrationsDirectory, "--max-risk", "safe",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "safe",
	)
	mustExec(t, ctx, pool, `UPDATE users SET legacy_note = $1 WHERE id = 1`, "preserve-me")

	// Explicit rename/review migration. Safe execution must fail closed.
	runCLI(t, ctx, ormBinary, 0,
		"diff", "--after", finalPath, "--id", upgradeMigrationID,
		"--renames", renamesPath, "--migrations", migrationsDirectory,
		"--max-risk", "review",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 1,
		"apply", "--max-risk", "safe",
	)
	assertRelationExists(t, ctx, pool, "users", true)
	assertRelationExists(t, ctx, pool, "accounts", false)

	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "review",
	)
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0, "status")
	assertRelationExists(t, ctx, pool, "users", false)
	assertRelationExists(t, ctx, pool, "accounts", true)
	assertColumnExists(t, ctx, pool, "accounts", "login_email", true)
	assertColumnExists(t, ctx, pool, "orders", "account_id", true)
	assertLegacyData(t, ctx, pool)

	exerciseTypedRuntime(t, ctx, pool)

	// Rollback restores the previous schema shape and preserves non-destructive data.
	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"rollback", "--steps", "1", "--yes",
	)
	assertRelationExists(t, ctx, pool, "accounts", false)
	assertRelationExists(t, ctx, pool, "users", true)
	assertColumnExists(t, ctx, pool, "users", "email", true)
	assertColumnExists(t, ctx, pool, "users", "legacy_note", true)
	assertColumnExists(t, ctx, pool, "orders", "user_id", true)
	var preserved string
	if err := pool.QueryRow(ctx, `SELECT legacy_note FROM users WHERE id = 1`).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if preserved != "preserve-me" {
		t.Fatalf("legacy note after rollback=%q", preserved)
	}

	runDatabaseCLI(t, ctx, ormBinary, databaseURL, migrationsDirectory, 0,
		"apply", "--max-risk", "review",
	)
	assertLegacyData(t, ctx, pool)

	verifyChecksumDriftRecovery(t, ctx, ormBinary, databaseURL, migrationsDirectory)
	verifyHistoryRecovery(t, ctx, ormBinary, databaseURL, migrationsDirectory, workspace)
	verifyDestructiveGate(t, ctx, pool, ormBinary, databaseURL, migrationsDirectory, destructivePath)
}

func exerciseTypedRuntime(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	recorder := &Recorder{}
	executor, err := NewExecutor(pool, recorder)
	if err != nil {
		t.Fatal(err)
	}

	legacy, found, err := FindAccountByEmail(ctx, executor, "legacy@example.test")
	if err != nil || !found {
		t.Fatalf("legacy account found=%t err=%v", found, err)
	}
	if legacy.ID != 1 || legacy.LegacyNote == nil || *legacy.LegacyNote != "preserve-me" {
		t.Fatalf("legacy account=%#v", legacy)
	}

	payload := `attack' OR 1=1; DROP TABLE accounts; --`
	displayName := "Payload User"
	created, err := CreateAccount(ctx, executor, Account{
		ID:          2,
		LoginEmail:  payload,
		DisplayName: &displayName,
		Active:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	matched, found, err := FindAccountByEmail(ctx, executor, payload)
	if err != nil || !found || matched.ID != created.ID {
		t.Fatalf("payload lookup account=%#v found=%t err=%v", matched, found, err)
	}
	assertRelationExists(t, ctx, pool, "accounts", true)

	if err := CreateAccountAndOrder(ctx, pool, recorder,
		Account{ID: 3, LoginEmail: "transaction@example.test", Active: true},
		Order{ID: 3, AccountID: 3, Total: 3400, Status: "paid"},
	); err != nil {
		t.Fatal(err)
	}
	if err := CreateAccountAndOrder(ctx, pool, recorder,
		Account{ID: 4, LoginEmail: "rollback@example.test", Active: true},
		Order{ID: 4, AccountID: 4, Total: -1, Status: "invalid"},
	); err == nil {
		t.Fatal("expected transaction check-constraint failure")
	}
	if _, found, err := FindAccountByEmail(ctx, executor, "rollback@example.test"); err != nil || found {
		t.Fatalf("rolled-back account found=%t err=%v", found, err)
	}

	if err := InsertOrdersBatch(ctx, executor, []Order{
		{ID: 20, AccountID: 2, Total: 500, Status: "pending"},
		{ID: 21, AccountID: 2, Total: 700, Status: "paid"},
	}); err != nil {
		t.Fatal(err)
	}
	updatedName := "Legacy Renamed"
	updated, err := SetDisplayName(ctx, executor, 1, &updatedName)
	if err != nil || updated.DisplayName == nil || *updated.DisplayName != updatedName {
		t.Fatalf("updated account=%#v err=%v", updated, err)
	}

	recorder.Reset()
	relations, err := AccountOrders().Load(ctx, executor, []Account{legacy, created, Account{ID: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if len(relations) != 3 || len(relations[0].Values) != 1 || len(relations[1].Values) != 2 || len(relations[2].Values) != 1 {
		t.Fatalf("relation results=%#v", relations)
	}
	events := recorder.Events()
	if len(events) != 1 {
		t.Fatalf("relation query count=%d events=%#v", len(events), events)
	}
	if strings.Contains(events[0].SQL, payload) {
		t.Fatalf("observer SQL leaked payload: %q", events[0].SQL)
	}

	deleted, err := query.DeleteOne(ctx, executor, query.Delete(OrderORM.Table).Where(OrderORM.ID.Eq(21)))
	if err != nil ||‘•±•Ñ•¹%€„ô€ÈÄì($%Ğ¹…Ñ…±˜ ‰‘•±•Ñ•½É‘•Èô”Ø•ÉÈô•Øˆ°‘•±•Ñ•°•ÉÈ¤(%ô(%™½È|°•Ù•¹Ğ€èôÉ…¹”É•½É‘•È¹Ù•¹ÑÌ ¤ì($%¥˜ÍÑÉ¥¹Ì¹½¹Ñ…¥¹Ì¡•Ù•¹Ğ¹ME0°Á…å±½…¤ì($$%Ğ¹…Ñ…±˜ ‰½‰Í•ÉÙ•È•Ù•¹Ğ±•…­•…ÉÕµ•¹Ğè€”Øˆ°•Ù•¹Ğ¤($%ô(%ô)ô()™Õ¹ŒÙ•É¥™å¡•­ÍÕµÉ¥™ÑI•½Ù•Éä¡Ğ€©Ñ•ÍÑ¥¹œ¹P°Ñà½¹Ñ•áĞ¹½¹Ñ•áĞ°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹ÌÍÑÉ¥¹œ¤ì(%Ğ¹!•±Á•È ¤(%Á…Ñ €èô™¥±•Á…Ñ ¹)½¥¸¡µ¥É…Ñ¥½¹Ì°ÕÁÉ…‘•5¥É…Ñ¥½¹%°€‰ÕÀ¹ÍÅ°ˆ¤(%½É¥¥¹…°°•ÉÈ€èô½Ì¹I•…‘¥±”¡Á…Ñ ¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%Ñ…µÁ•É•€èô…ÁÁ•¹¡…ÁÁ•¹¡mu‰åÑ”¡¹¥°¤°½É¥¥¹…°¸¸¸¤°mu‰åÑ” ˆ´´Ñ…µÁ•É•‘q¸ˆ¤¸¸¸¤(%¥˜•ÉÈ€èô½Ì¹]É¥Ñ•¥±”¡Á…Ñ °Ñ…µÁ•É•°€Á¼ØĞĞ¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%ÉÕ¹…Ñ…‰…Í•1$¡Ğ°Ñà°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹Ì°€Ä°€‰ÍÑ…ÑÕÌˆ¤(%¥˜•ÉÈ€èô½Ì¹]É¥Ñ•¥±”¡Á…Ñ °½É¥¥¹…°°€Á¼ØĞĞ¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%ÉÕ¹…Ñ…‰…Í•1$¡Ğ°Ñà°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹Ì°€À°€‰ÍÑ…ÑÕÌˆ¤)ô()™Õ¹ŒÙ•É¥™å!¥ÍÑ½ÉåI•½Ù•Éä¡Ğ€©Ñ•ÍÑ¥¹œ¹P°Ñà½¹Ñ•áĞ¹½¹Ñ•áĞ°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹Ì°İ½É­ÍÁ…”ÍÑÉ¥¹œ¤ì(%Ğ¹!•±Á•È ¤(%½É¥¥¹…°€èô™¥±•Á…Ñ ¹)½¥¸¡µ¥É…Ñ¥½¹Ì°¥¹¥Ñ¥…±5¥É…Ñ¥½¹%¤(%‰…­ÕÀ€èô™¥±•Á…Ñ ¹)½¥¸¡İ½É­ÍÁ…”°¥¹¥Ñ¥…±5¥É…Ñ¥½¹%¬ˆ¹‰…­ÕÀˆ¤(%¥˜•ÉÈ€èô½Ì¹I•¹…µ”¡½É¥¥¹…°°‰…­ÕÀ¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%ÉÕ¹…Ñ…‰…Í•1$¡Ğ°Ñà°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹Ì°€Ä°€‰ÍÑ…ÑÕÌˆ¤(%¥˜•ÉÈ€èô½Ì¹I•¹…µ”¡‰…­ÕÀ°½É¥¥¹…°¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%ÉÕ¹…Ñ…‰…Í•1$¡Ğ°Ñà°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹Ì°€À°€‰ÍÑ…ÑÕÌˆ¤)ô()™Õ¹ŒÙ•É¥™å•ÍÑÉÕÑ¥Ù•…Ñ”¡Ğ€©Ñ•ÍÑ¥¹œ¹P°Ñà½¹Ñ•áĞ¹½¹Ñ•áĞ°Á½½°€©ÁáÁ½½°¹A½½°°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹Ì°…™Ñ•ÈÍÑÉ¥¹œ¤ì(%Ğ¹!•±Á•È ¤(%ÉÕ¹1$¡Ğ°Ñà°‰¥¹…Éä°€Ä°($$‰‘¥™˜ˆ°€ˆ´µ…™Ñ•Èˆ°…™Ñ•È°€ˆ´µ¥ˆ°‘•ÍÑÉÕÑ¥Ù•5¥É…Ñ¥½¹%°($$ˆ´µµ¥É…Ñ¥½¹Ìˆ°µ¥É…Ñ¥½¹Ì°€ˆ´µµ…àµÉ¥Í¬ˆ°€‰Í…™”ˆ°($¤(%ÉÕ¹1$¡Ğ°Ñà°‰¥¹…Éä°€À°($$‰‘¥™˜ˆ°€ˆ´µ…™Ñ•Èˆ°…™Ñ•È°€ˆ´µ¥ˆ°‘•ÍÑÉÕÑ¥Ù•5¥É…Ñ¥½¹%°($$ˆ´µµ¥É…Ñ¥½¹Ìˆ°µ¥É…Ñ¥½¹Ì°€ˆ´µµ…àµÉ¥Í¬ˆ°€‰‘•ÍÑÉÕÑ¥Ù”ˆ°($¤(%ÉÕ¹…Ñ…‰…Í•1$¡Ğ°Ñà°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹Ì°€Ä°($$‰…ÁÁ±äˆ°€ˆ´µµ…àµÉ¥Í¬ˆ°€‰Í…™”ˆ°($¤(%ÉÕ¹…Ñ…‰…Í•1$¡Ğ°Ñà°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹Ì°€È°($$‰…ÁÁ±äˆ°€ˆ´µµ…àµÉ¥Í¬ˆ°€‰‘•ÍÑÉÕÑ¥Ù”ˆ°($¤(%…ÍÍ•ÉÑ½±Õµ¹á¥ÍÑÌ¡Ğ°Ñà°Á½½°°€‰…½Õ¹ÑÌˆ°€‰±•…å}¹½Ñ”ˆ°ÑÉÕ”¤(%¥˜•ÉÈ€èô½Ì¹I•µ½Ù•±°¡™¥±•Á…Ñ ¹)½¥¸¡µ¥É…Ñ¥½¹Ì°‘•ÍÑÉÕÑ¥Ù•5¥É…Ñ¥½¹%¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%ÉÕ¹…Ñ…‰…Í•1$¡Ğ°Ñà°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹Ì°€À°€‰ÍÑ…ÑÕÌˆ¤)ô()™Õ¹ŒÉÕ¹…Ñ…‰…Í•1$¡Ğ€©Ñ•ÍÑ¥¹œ¹P°Ñà½¹Ñ•áĞ¹½¹Ñ•áĞ°‰¥¹…Éä°‘…Ñ…‰…Í•UI0°µ¥É…Ñ¥½¹ÌÍÑÉ¥¹œ°•áÁ•Ñ•‘á¥Ğ¥¹Ğ°½µµ…¹ÍÑÉ¥¹œ°…ÉÕµ•¹ÑÌ€¸¸¹ÍÑÉ¥¹œ¤½µµ…¹‘I•ÍÕ±Ğì(%Ğ¹!•±Á•È ¤(%…ÉÌ€èômuÍÑÉ¥¹ì($%½µµ…¹°($$ˆ´µ‘…Ñ…‰…Í”µÕÉ°ˆ°‘…Ñ…‰…Í•UI0°($$ˆ´µµ¥É…Ñ¥½¹Ìˆ°µ¥É…Ñ¥½¹Ì°($$ˆ´µ¡¥ÍÑ½ÉäµÍ¡•µ„ˆ°€‰ÁÕ‰±¥Œˆ°($$ˆ´µ¡¥ÍÑ½ÉäµÑ…‰±”ˆ°€‰}}Í•Ù±Õµ•¹}É}µ¥É…Ñ¥½¹Ìˆ°($$ˆ´µ±½¬µ­•äˆ°€ˆäÄÀØÀÀÄˆ°($$ˆ´µÑ¥µ•½ÕĞˆ°€ˆÌÁÌˆ°(%ô(%…ÉÌ€ô…ÁÁ•¹¡…ÉÌ°…ÉÕµ•¹ÑÌ¸¸¸¤(%É•ÑÕÉ¸ÉÕ¹1$¡Ğ°Ñà°‰¥¹…Éä°•áÁ•Ñ•‘á¥Ğ°…ÉÌ¸¸¸¤)ô()ÑåÁ”½µµ…¹‘I•ÍÕ±ĞÍÑÉÕĞì(%ÍÑ‘½ÕĞÍÑÉ¥¹œ(%ÍÑ‘•ÉÈÍÑÉ¥¹œ)ô()™Õ¹ŒÉÕ¹1$¡Ğ€©Ñ•ÍÑ¥¹œ¹P°Á…É•¹Ğ½¹Ñ•áĞ¹½¹Ñ•áĞ°‰¥¹…ÉäÍÑÉ¥¹œ°•áÁ•Ñ•‘á¥Ğ¥¹Ğ°…ÉÕµ•¹ÑÌ€¸¸¹ÍÑÉ¥¹œ¤½µµ…¹‘I•ÍÕ±Ğì(%Ğ¹!•±Á•È ¤(%Ñà°…¹•°€èô½¹Ñ•áĞ¹]¥Ñ¡Q¥µ•½ÕĞ¡Á…É•¹Ğ°€ĞÔ©Ñ¥µ”¹M•½¹¤(%‘•™•È…¹•° ¤(%½µµ…¹€èô•á•Œ¹½µµ…¹‘½¹Ñ•áĞ¡Ñà°‰¥¹…Éä°…ÉÕµ•¹ÑÌ¸¸¸¤(%½µµ…¹¹¹Ø€ô½Ì¹¹Ù¥É½¸ ¤(%Ù…ÈÍÑ‘½ÕĞ‰åÑ•Ì¹	Õ™™•È(%Ù…ÈÍÑ‘•ÉÈ‰åÑ•Ì¹	Õ™™•È(%½µµ…¹¹MÑ‘½ÕĞ€ô€™ÍÑ‘½ÕĞ(%½µµ…¹¹MÑ‘•ÉÈ€ô€™ÍÑ‘•ÉÈ(%•ÉÈ€èô½µµ…¹¹IÕ¸ ¤(%•á¥Ñ½‘”€èô€À(%¥˜•ÉÈ€„ô¹¥°ì($%Ù…È•á¥ÑÉÉ½È€©•á•Œ¹á¥ÑÉÉ½È($%¥˜•ÉÉ½ÉÌ¹Ì¡•ÉÈ°€™•á¥ÑÉÉ½È¤ì($$%•á¥Ñ½‘”€ô•á¥ÑÉÉ½È¹á¥Ñ½‘” ¤($%ô•±Í”ì($$%Ğ¹…Ñ…±˜ ‰ÉÕ¸€•Ì€•Ìè€•Øˆ°‰¥¹…Éä°ÍÑÉ¥¹Ì¹)½¥¸¡…ÉÕµ•¹ÑÌ°€ˆ€ˆ¤°•ÉÈ¤($%ô(%ô(%¥˜•á¥Ñ½‘”€„ô•áÁ•Ñ•‘á¥Ğì($%Ğ¹…Ñ…±˜ ˆ•Ì€•Ì•á¥Ğô•İ…¹Ğô•‘q¹ÍÑ‘½ÕĞéq¸•Íq¹ÍÑ‘•ÉÈéq¸•Ìˆ°‰¥¹…Éä°ÍÑÉ¥¹Ì¹)½¥¸¡…ÉÕµ•¹ÑÌ°€ˆ€ˆ¤°•á¥Ñ½‘”°•áÁ•Ñ•‘á¥Ğ°ÍÑ‘½ÕĞ¹MÑÉ¥¹œ ¤°ÍÑ‘•ÉÈ¹MÑÉ¥¹œ ¤¤(%ô(%É•ÑÕÉ¸½µµ…¹‘I•ÍÕ±ÑíÍÑ‘½ÕĞèÍÑ‘½ÕĞ¹MÑÉ¥¹œ ¤°ÍÑ‘•ÉÈèÍÑ‘•ÉÈ¹MÑÉ¥¹œ ¥ô)ô()™Õ¹ŒÉ•Í•Ñ…Ñ…‰…Í”¡Ğ€©Ñ•ÍÑ¥¹œ¹P°Ñà½¹Ñ•áĞ¹½¹Ñ•áĞ°Á½½°€©ÁáÁ½½°¹A½½°¤ì(%Ğ¹!•±Á•È ¤(%µÕÍÑá•Œ¡Ğ°Ñà°Á½½°°I=@M!5%a%MQLÁÕ‰±¥ŒM„¤(%µÕÍÑá•Œ¡Ğ°Ñà°Á½½°°IQM!5ÁÕ‰±¥€¤)ô()™Õ¹ŒµÕÍÑá•Œ¡Ğ€©Ñ•ÍÑ¥¹œ¹P°Ñà½¹Ñ•áĞ¹½¹Ñ•áĞ°Á½½°€©ÁáÁ½½°¹A½½°°ÍÅ°ÍÑÉ¥¹œ°…ÉÕµ•¹ÑÌ€¸¸¹…¹ä¤ì(%Ğ¹!•±Á•È ¤(%¥˜|°•ÉÈ€èôÁ½½°¹á•Œ¡Ñà°ÍÅ°°…ÉÕµ•¹ÑÌ¸¸¸¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô)ô()™Õ¹ŒÁ…­…•¥É•Ñ½Éä¡Ğ€©Ñ•ÍÑ¥¹œ¹P¤ÍÑÉ¥¹œì(%Ğ¹!•±Á•È ¤(%|°™¥±•¹…µ”°|°½¬€èôÉÕ¹Ñ¥µ”¹…±±•È À¤(%¥˜€…½¬ì($%Ğ¹…Ñ…° ‰É•Í½±Ù”É…ÁÀÍ½ÕÉ”‘¥É•Ñ½Éäˆ¤(%ô(%É•ÑÕÉ¸™¥±•Á…Ñ ¹¥È¡™¥±•¹…µ”¤)ô()™Õ¹ŒİÉ¥Ñ•1½…‘•‘M¹…ÁÍ¡½Ğ¡Ğ€©Ñ•ÍÑ¥¹œ¹P°‘¥É•Ñ½Éä°¹…µ”ÍÑÉ¥¹œ°±½…™Õ¹Œ ¤€¡µ¥É…Ñ¥½¸¹M¹…ÁÍ¡½Ğ°•ÉÉ½È¤¤ÍÑÉ¥¹œì(%Ğ¹!•±Á•È ¤(%Í¹…ÁÍ¡½Ğ°•ÉÈ€èô±½… ¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%‘…Ñ„°•ÉÈ€èôÍ¹…ÁÍ¡½Ğ¹5…ÉÍ¡…° ¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%Á…Ñ €èô™¥±•Á…Ñ ¹)½¥¸¡‘¥É•Ñ½Éä°¹…µ”¤(%¥˜•ÉÈ€èô½Ì¹]É¥Ñ•¥±”¡Á…Ñ °‘…Ñ„°€Á¼ØĞĞ¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%É•ÑÕÉ¸Á…Ñ )ô()™Õ¹ŒİÉ¥Ñ•I•¹…µ•Ì¡Ğ€©Ñ•ÍÑ¥¹œ¹P°‘¥É•Ñ½ÉäÍÑÉ¥¹œ°½ÁÑ¥½¹Ìµ¥É…Ñ¥½¸¹¥™™=ÁÑ¥½¹Ì¤ÍÑÉ¥¹œì(%Ğ¹!•±Á•È ¤(%Á…å±½…€èôÍÑÉÕĞì($%Y•ÉÍ¥½¸¥¹Ğ€€€€€€€€€€€€€€©Í½¸è‰Ù•ÉÍ¥½¸‰€($%I•¹…µ•Ìmuµ¥É…Ñ¥½¸¹I•¹…µ”©Í½¸è‰É•¹…µ•Ì‰€(%õíY•ÉÍ¥½¸è€Ä°I•¹…µ•Ìè½ÁÑ¥½¹Ì¹I•¹…µ•Íô(%‘…Ñ„°•ÉÈ€èô©Í½¸¹5…ÉÍ¡…±%¹‘•¹Ğ¡Á…å±½…°€ˆˆ°€ˆ€€ˆ¤(%¥˜•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%‘…Ñ„€ô…ÁÁ•¹¡‘…Ñ„°€q¸œ¤(%Á…Ñ €èô™¥±•Á…Ñ ¹)½¥¸¡‘¥É•Ñ½Éä°€‰É•¹…µ•Ì¹©Í½¸ˆ¤(%¥˜•ÉÈ€èô½Ì¹]É¥Ñ•¥±”¡Á…Ñ °‘…Ñ„°€Á¼ØĞĞ¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%É•ÑÕÉ¸Á…Ñ )ô()™Õ¹Œ…ÍÍ•ÉÑ1•…å…Ñ„¡Ğ€©Ñ•ÍÑ¥¹œ¹P°Ñà½¹Ñ•áĞ¹½¹Ñ•áĞ°Á½½°€©ÁáÁ½½°¹A½½°¤ì(%Ğ¹!•±Á•È ¤(%Ù…È•µ…¥°ÍÑÉ¥¹œ(%Ù…È¹½Ñ”€©ÍÑÉ¥¹œ(%¥˜•ÉÈ€èôÁ½½°¹EÕ•ÉåI½Ü¡Ñà°M1P±½¥¹}•µ…¥°°±•…å}¹½Ñ”I=4…½Õ¹ÑÌ]!I¥€ô€Å€¤¹M…¸ ™•µ…¥°°€™¹½Ñ”¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%¥˜•µ…¥°€„ô€‰±•…å•á…µÁ±”¹Ñ•ÍĞˆñğ¹½Ñ”€ôô¹¥°ñğ€©¹½Ñ”€„ô€‰ÁÉ•Í•ÉÙ”µµ”ˆì($%Ğ¹…Ñ…±˜ ‰ÕÁÉ…‘•‘…Ñ„•µ…¥°ô•Ä¹½Ñ”ô•Øˆ°•µ…¥°°¹½Ñ”¤(%ô(%Ù…È…½Õ¹Ñ%°Ñ½Ñ…°¥¹ĞØĞ(%¥˜•ÉÈ€èôÁ½½°¹EÕ•ÉåI½Ü¡Ñà°M1P…½Õ¹Ñ}¥°Ñ½Ñ…°I=4½É‘•ÉÌ]!I¥€ô€Å€¤¹M…¸ ™…½Õ¹Ñ%°€™Ñ½Ñ…°¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%¥˜…½Õ¹Ñ%€„ô€ÄñğÑ½Ñ…°€„ô€ÄÈÀÀì($%Ğ¹…Ñ…±˜ ‰ÕÁÉ…‘•½É‘•È…½Õ¹Ğô•Ñ½Ñ…°ô•ˆ°…½Õ¹Ñ%°Ñ½Ñ…°¤(%ô)ô()™Õ¹Œ…ÍÍ•ÉÑI•±…Ñ¥½¹á¥ÍÑÌ¡Ğ€©Ñ•ÍÑ¥¹œ¹P°Ñà½¹Ñ•áĞ¹½¹Ñ•áĞ°Á½½°€©ÁáÁ½½°¹A½½°°É•±…Ñ¥½¸ÍÑÉ¥¹œ°•áÁ•Ñ•‰½½°¤ì(%Ğ¹!•±Á•È ¤(%Ù…È•á¥ÍÑÌ‰½½°(%¥˜•ÉÈ€èôÁ½½°¹EÕ•ÉåI½Ü¡Ñà°M1Pa%MQL€ ($%M1P€ÄI=4Á}±…ÍÌŒ)=%8Á}¹…µ•ÍÁ…”¸=8¸¹½¥€ôŒ¹É•±¹…µ•ÍÁ…”($%]!I¸¹¹ÍÁ¹…µ”€ô€ÁÕ‰±¥Œœ9Œ¹É•±¹…µ”€ô€Ä($¥€°É•±…Ñ¥½¸¤¹M…¸ ™•á¥ÍÑÌ¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%¥˜•á¥ÍÑÌ€„ô•áÁ•Ñ•ì($%Ğ¹…Ñ…±˜ ‰É•±…Ñ¥½¸€•Ä•á¥ÍÑÌô•Ğİ…¹Ğô•Ğˆ°É•±…Ñ¥½¸°•á¥ÍÑÌ°•áÁ•Ñ•¤(%ô)ô()™Õ¹Œ…ÍÍ•ÉÑ½±Õµ¹á¥ÍÑÌ¡Ğ€©Ñ•ÍÑ¥¹œ¹P°Ñà½¹Ñ•áĞ¹½¹Ñ•áĞ°Á½½°€©ÁáÁ½½°¹A½½°°Ñ…‰±”°½±Õµ¸ÍÑÉ¥¹œ°•áÁ•Ñ•‰½½°¤ì(%Ğ¹!•±Á•È ¤(%Ù…È•á¥ÍÑÌ‰½½°(%¥˜•ÉÈ€èôÁ½½°¹EÕ•ÉåI½Ü¡Ñà°M1Pa%MQL€ ($%M1P€ÄI=4¥¹™½Éµ…Ñ¥½¹}Í¡•µ„¹½±Õµ¹Ì($%]!IÑ…‰±•}Í¡•µ„€ô€ÁÕ‰±¥Œœ9Ñ…‰±•}¹…µ”€ô€Ä9½±Õµ¹}¹…µ”€ô€È($¥€°Ñ…‰±”°½±Õµ¸¤¹M…¸ ™•á¥ÍÑÌ¤ì•ÉÈ€„ô¹¥°ì($%Ğ¹…Ñ…°¡•ÉÈ¤(%ô(%¥˜•á¥ÍÑÌ€„ô•áÁ•Ñ•ì($%Ğ¹…Ñ…±˜ ‰½±Õµ¸€•Ì¸•Ì•á¥ÍÑÌô•Ğİ…¹Ğô•Ğˆ°Ñ…‰±”°½±Õµ¸°•á¥ÍÑÌ°•áÁ•Ñ•¤(%ô)ô()™Õ¹Œá…µÁ±•}É•±•…Í•…¹‘¥‘…Ñ•]½É­™±½Ü ¤ì(%™µĞ¹AÉ¥¹Ñ±¸ ‰•¹•É…Ñ”€´ø‘¥™˜€´øÙ…±¥‘…Ñ”€´ø…ÁÁ±ä€´øÑåÁ•ÉÕ¹Ñ¥µ”€´øÉ½±±‰…¬€´øÉ•½Ù•Éäˆ¤($¼¼=ÕÑÁÕĞè•¹•É…Ñ”€´ø‘¥™˜€´øÙ…±¥‘…Ñ”€´ø…ÁÁ±ä€´øÑåÁ•ÉÕ¹Ñ¥µ”€´øÉ½±±‰…¬€´øÉ•½Ù•Éä)ô