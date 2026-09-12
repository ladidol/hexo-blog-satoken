package platform_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"blog-go/internal/platform"
)

func TestSQLiteMigrationIsAtomicPersistentAndVersioned(t *testing.T) {
	cfg := config()
	cfg.DataDir = t.TempDir()
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Stats().MaxOpenConnections != 1 {
		t.Fatal("writes are not serialized")
	}
	for name, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "busy_timeout": "5000"} {
		var got string
		if err := db.QueryRow("PRAGMA " + name).Scan(&got); err != nil || got != want {
			t.Fatalf("%s=%s: %v", name, got, err)
		}
	}
	seedCalls := 0
	seed := func(ctx context.Context, tx *sql.Tx) error {
		seedCalls++
		_, err := tx.ExecContext(ctx, "INSERT INTO entries VALUES(1,'seed')")
		return err
	}
	schemas := []string{"CREATE TABLE entries(id INTEGER PRIMARY KEY, content TEXT NOT NULL)"}
	if err := platform.Migrate(context.Background(), db, schemas, seed); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE entries SET content='edited' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err := platform.Migrate(context.Background(), db, schemas, seed); err != nil {
		t.Fatal(err)
	}
	if seedCalls != 1 {
		t.Fatal("seed resurrected/overwrote user data")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var content string
	if err := db.QueryRow("SELECT content FROM entries WHERE id=1").Scan(&content); err != nil || content != "edited" {
		t.Fatalf("restart lost data: %s %v", content, err)
	}
	if _, err := db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	if err := platform.Migrate(context.Background(), db, schemas, seed); err == nil {
		t.Fatal("accepted newer database version")
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "blog.db")); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteMigrationRollsBackDDLSeedAndCancellation(t *testing.T) {
	cfg := config()
	cfg.DataDir = t.TempDir()
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	failure := errors.New("seed rejected")
	err = platform.Migrate(context.Background(), db, []string{"CREATE TABLE rejected(id INTEGER)"},
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "INSERT INTO rejected VALUES(1)"); err != nil {
				t.Fatal(err)
			}
			return failure
		})
	if !errors.Is(err, failure) {
		t.Fatalf("lost failure identity: %v", err)
	}
	var count, version int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='rejected'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if count != 0 || version != 0 {
		t.Fatal("partial first migration committed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := platform.Migrate(ctx, db, []string{"CREATE TABLE rejected(id INTEGER)"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestHealthChecksDatabaseWithoutConfiguration(t *testing.T) {
	cfg := config()
	cfg.DataDir = t.TempDir()
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	engine := router(t, []platform.Route{{Spec: platform.RouteSpec{Method: "GET", Path: "/healthz", Operation: "Health", Policy: "public"}, Raw: platform.Health(db)}}, platform.Callbacks{})
	response := call(engine, "GET", "/healthz", "", nil)
	if response.Code != 200 || response.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("health: %d %s", response.Code, response.Body)
	}
	_ = db.Close()
	response = call(engine, "GET", "/healthz", "", nil)
	if response.Code != 503 || response.Body.String() != "{\"status\":\"unavailable\"}\n" {
		t.Fatalf("failed health: %d %s", response.Code, response.Body)
	}
}

func TestHealthDoesNotDependOnAuthenticationAvailability(t *testing.T) {
	cfg := config()
	cfg.DataDir = t.TempDir()
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine := router(t, []platform.Route{{Spec: platform.RouteSpec{Method: "GET", Path: "/healthz", Operation: "Health", Policy: "public"}, Raw: platform.Health(db)}},
		platform.Callbacks{Resolve: func(*http.Request) (platform.Actor, error) {
			return platform.Actor{}, errors.New("unavailable identity service")
		}})
	response := call(engine, "GET", "/healthz", "", nil)
	if response.Code != 200 || response.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("health depends on identity: %d %s", response.Code, response.Body)
	}
	_ = db.Close()
	response = call(engine, "GET", "/healthz", "", nil)
	if response.Code != 503 {
		t.Fatalf("database failure hidden by identity adapter: %d", response.Code)
	}
}
