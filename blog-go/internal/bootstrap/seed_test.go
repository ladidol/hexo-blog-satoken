package bootstrap_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"blog-go/internal/analytics"
	"blog-go/internal/app"
	"blog-go/internal/article"
	"blog-go/internal/bootstrap"
	"blog-go/internal/catalog"
	"blog-go/internal/chat"
	"blog-go/internal/community"
	"blog-go/internal/engagement"
	"blog-go/internal/files"
	"blog-go/internal/identity"
	"blog-go/internal/mail"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
)

func seedConfig(t *testing.T) platform.Config {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	return platform.Config{
		DataDir: t.TempDir(), PublicURL: "http://localhost:8888", FrontendURL: "http://localhost:8080",
		TimeZone: location, Values: map[string]string{"BLOG_UPLOAD_MODE": "local"},
	}
}

func TestChangedDefaultLoginDoesNotRewriteExistingAccounts(t *testing.T) {
	cfg := seedConfig(t)
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(t, db, cfg); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.DataDir, "initial-admin.txt")
	initial, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(string(initial), "username=localadmin@example.test\n", "username=local-admin\n", 1)
	if _, err := db.Exec("UPDATE tb_user_auth SET username='local-admin' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrate(t, db, cfg); err != nil {
		t.Fatal(err)
	}
	var username string
	if err := db.QueryRow("SELECT username FROM tb_user_auth WHERE id=1").Scan(&username); err != nil || username != "local-admin" {
		t.Fatal("existing username was changed")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != legacy {
		t.Fatal("existing credential file was reset")
	}
}

func schemas() []string {
	return []string{identity.Schema, catalog.Schema, article.Schema, community.Schema, media.Schema,
		site.Schema, analytics.Schema, mail.Schema, chat.Schema, engagement.Schema}
}

func migrate(t *testing.T, db *sql.DB, cfg platform.Config) error {
	t.Helper()
	return platform.Migrate(context.Background(), db, schemas(), func(ctx context.Context, tx *sql.Tx) error {
		return bootstrap.Seed(ctx, tx, cfg)
	})
}

func TestSeedCreatesUsablePrivateLocalDataOnce(t *testing.T) {
	cfg := seedConfig(t)
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(t, db, cfg); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]int{
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'tb_%'": 28,
		"SELECT count(*) FROM tb_menu": 33,
		"SELECT count(*) FROM tb_role": 3,
		"SELECT count(*) FROM tb_user_auth WHERE username='localadmin@example.test' AND login_type=1":                                                1,
		"SELECT count(*) FROM tb_article WHERE status=1 AND is_delete=0":                                                                             1,
		"SELECT count(*) FROM tb_talk WHERE status=1":                                                                                                1,
		"SELECT count(*) FROM tb_photo_album WHERE status=1 AND is_delete=0":                                                                         1,
		"SELECT count(*) FROM tb_role_resource rr JOIN tb_role r ON rr.role_id=r.id WHERE r.role_label='user'":                                       0,
		"SELECT count(*) FROM tb_role_menu rm JOIN tb_role r ON rm.role_id=r.id WHERE r.role_label='test'":                                           2,
		"SELECT count(*) FROM tb_role_resource rr JOIN tb_role r ON rr.role_id=r.id WHERE r.role_label='test'":                                       2,
		"SELECT count(*) FROM tb_resource resource LEFT JOIN tb_role_resource rr ON rr.resource_id=resource.id AND rr.role_id=1 WHERE rr.id IS NULL": 0,
	} {
		var got int
		if err := db.QueryRow(query).Scan(&got); err != nil || got != want {
			t.Fatalf("seed query got %d want %d: %v (%s)", got, want, err, query)
		}
	}
	var foreignKeyViolations int
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		foreignKeyViolations++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if foreignKeyViolations != 0 {
		t.Fatal("seed has invalid relationships")
	}
	resources, err := db.Query("SELECT resource.request_method,resource.url FROM tb_role_resource rr JOIN tb_resource resource ON rr.resource_id=resource.id WHERE rr.role_id=3")
	if err != nil {
		t.Fatal(err)
	}
	for resources.Next() {
		var method, path string
		if err := resources.Scan(&method, &path); err != nil {
			t.Fatal(err)
		}
		if method != "GET" || (path != "/admin" && path != "/admin/user/menus") {
			t.Fatalf("test role overprivileged: %s %s", method, path)
		}
	}
	if err := resources.Err(); err != nil {
		t.Fatal(err)
	}
	_ = resources.Close()
	expectedResources := 0
	for _, group := range [][]platform.RouteSpec{identity.RouteSpecs, article.RouteSpecs, catalog.RouteSpecs,
		community.RouteSpecs, media.RouteSpecs, site.RouteSpecs, analytics.RouteSpecs, files.RouteSpecs} {
		for _, spec := range group {
			if spec.Policy != "admin" {
				continue
			}
			expectedResources++
			var count int
			if err := db.QueryRow("SELECT count(*) FROM tb_resource WHERE request_method=? AND url=?", spec.Method, spec.Path).Scan(&count); err != nil || count != 1 {
				t.Fatalf("management resource missing/duplicated: %s %s", spec.Method, spec.Path)
			}
		}
	}
	var actualResources int
	if err := db.QueryRow("SELECT count(*) FROM tb_resource").Scan(&actualResources); err != nil || actualResources != expectedResources {
		t.Fatal("seeded resources differ from the canonical registered management routes")
	}
	credentialPath := filepath.Join(cfg.DataDir, "initial-admin.txt")
	credential, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(credentialPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("initial credentials are not private")
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(credential), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	if values["username"] != "localadmin@example.test" || len(values["password"]) < 32 {
		t.Fatal("missing random local credential")
	}
	var encodedPassword string
	if err := db.QueryRow("SELECT password FROM tb_user_auth WHERE username='localadmin@example.test'").Scan(&encodedPassword); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(encodedPassword), []byte(values["password"])); err != nil {
		t.Fatal("generated credential does not authenticate")
	}
	if cost, err := bcrypt.Cost([]byte(encodedPassword)); err != nil || cost != 10 {
		t.Fatal("incorrect password cost")
	}
	var rawConfig string
	if err := db.QueryRow("SELECT config FROM tb_website_config WHERE id=1").Scan(&rawConfig); err != nil {
		t.Fatal(err)
	}
	var website map[string]any
	if err := json.Unmarshal([]byte(rawConfig), &website); err != nil {
		t.Fatal(err)
	}
	if len(website) != 24 || website["websiteName"] != "本地博客" || website["isCommentReview"] != float64(1) ||
		website["isMessageReview"] != float64(1) || website["isChatRoom"] != float64(1) ||
		website["isEmailNotice"] != float64(0) || website["websocketUrl"] != "ws://localhost:8888/websocket" {
		t.Fatalf("incomplete website configuration: %v", website)
	}
	for _, file := range []string{"avatar.svg", "cover.svg"} {
		if _, err := os.Stat(filepath.Join(cfg.DataDir, "files", "defaults", file)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("UPDATE tb_website_config SET config='{\"edited\":true}' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM tb_page WHERE page_label='about'"); err != nil {
		t.Fatal(err)
	}
	if err := migrate(t, db, cfg); err != nil {
		t.Fatal(err)
	}
	credentialAfter, _ := os.ReadFile(credentialPath)
	if string(credentialAfter) != string(credential) {
		t.Fatal("restart changed initial password")
	}
	if err := db.QueryRow("SELECT config FROM tb_website_config WHERE id=1").Scan(&rawConfig); err != nil || rawConfig != "{\"edited\":true}" {
		t.Fatal("restart overwrote config")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM tb_page WHERE page_label='about'").Scan(&count); err != nil || count != 0 {
		t.Fatal("restart resurrected deleted page")
	}
}

func TestSeedFailureRollsBackAndPreservesExistingFiles(t *testing.T) {
	cfg := seedConfig(t)
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	existing := filepath.Join(cfg.DataDir, "initial-admin.txt")
	if err := os.WriteFile(existing, []byte("existing-private-file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrate(t, db, cfg); err == nil {
		t.Fatal("seed overwrote an existing credential file")
	}
	var tables int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE 'tb_%'").Scan(&tables); err != nil || tables != 0 {
		t.Fatal("failed seed committed partial database")
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "existing-private-file" {
		t.Fatal("seed damaged an existing private file")
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "files", "defaults", "avatar.svg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed seed left a partially created asset")
	}
}

func TestSeedFilesRollBackWhenTransactionCommitFails(t *testing.T) {
	cfg := seedConfig(t)
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = platform.Migrate(context.Background(), db, schemas(), func(ctx context.Context, tx *sql.Tx) error {
		if err := bootstrap.Seed(ctx, tx, cfg); err != nil {
			return err
		}
		// This deferred foreign key violation surfaces at commit, after Seed returned success.
		_, err := tx.ExecContext(ctx, "INSERT INTO tb_user_role(user_id,role_id) VALUES(999999,1)")
		return err
	})
	if err == nil {
		t.Fatal("invalid seed transaction committed")
	}
	for _, file := range []string{"initial-admin.txt", "files/defaults/avatar.svg", "files/defaults/cover.svg"} {
		if _, err := os.Stat(filepath.Join(cfg.DataDir, file)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed commit retained seed file %s", file)
		}
	}
	if err := migrate(t, db, cfg); err != nil {
		t.Fatalf("failed migration cannot be retried: %v", err)
	}
}

func TestAppRunCancellationAndListenerFailureReleaseDatabase(t *testing.T) {
	cfg := seedConfig(t)
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	instance := &app.App{Config: cfg, DB: db, HTTP: &http.Server{Addr: "127.0.0.1:0"}}
	if err := instance.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err == nil {
		t.Fatal("cancelled startup retained database")
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	db, err = platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	instance = &app.App{Config: cfg, DB: db, HTTP: &http.Server{Addr: occupied.Addr().String()}}
	if err := instance.Run(context.Background()); err == nil {
		t.Fatal("listener collision reported success")
	}
	if err := db.Ping(); err == nil {
		t.Fatal("listener failure retained database")
	}
}

func TestAppRunServesAndStopsGracefully(t *testing.T) {
	cfg := seedConfig(t)
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	instance := &app.App{Config: cfg, DB: db, HTTP: &http.Server{Addr: addr, Handler: platform.Health(db)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan error, 1)
	go func() { ended <- instance.Run(ctx) }()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(3 * time.Second)
	served := false
	for time.Now().Before(deadline) {
		select {
		case err := <-ended:
			t.Fatalf("server exited before serving: %v", err)
		default:
		}
		response, err := client.Get("http://" + addr + "/healthz")
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != 200 || string(body) != "{\"status\":\"ok\"}\n" {
				t.Fatal("application did not serve database health")
			}
			served = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !served {
		t.Fatal("application did not start")
	}
	cancel()
	select {
	case err := <-ended:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("application shutdown exceeded the five-second budget")
	}
	if err := db.Ping(); err == nil {
		t.Fatal("shutdown did not release the database")
	}
}
