package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"blog-go/internal/analytics"
	"blog-go/internal/engagement"
	"blog-go/internal/files"
	"blog-go/internal/identity"
	"blog-go/internal/mail"
	"blog-go/internal/platform"
	"blog-go/internal/site"
	"blog-go/internal/social"
)

func TestNonAccountBusinessFailuresKeepOriginalCodes(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/articles/999999/like", "/comments/999999/like", "/talks/999999/like"} {
		f.fail(t, f.user, "POST", path, nil, platform.CodeFail)
	}
	for _, path := range []string{"/admin/menus/999999", "/admin/resources/999999"} {
		f.fail(t, f.admin, "DELETE", path, nil, platform.CodeFail)
	}
	f.fail(t, f.guest, "GET", "/comments/999999/replies", nil, platform.CodeFail)
	f.fail(t, f.admin, "POST", "/admin/role", map[string]any{"roleName": "管理员", "roleLabel": "another"}, platform.CodeFail)
	page := map[string]any{"pageName": "First", "pageLabel": "integration-unique", "pageCover": f.cfg.PublicURL + "/files/defaults/cover.svg"}
	f.ok(t, f.admin, "POST", "/admin/pages", page)
	f.fail(t, f.admin, "POST", "/admin/pages", page, platform.CodeFail)
}

func TestAccessBoundaries(t *testing.T) {
	f := newFixture(t)
	before := databaseDigest(t, f)
	for _, spec := range specs() {
		if spec.Policy == "public" {
			continue
		}
		method := spec.Method
		if method == "ANY" {
			method = "POST"
		}
		path := regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(spec.Path, "1")
		f.fail(t, f.guest, method, path, map[string]any{}, platform.CodeFail)
		if spec.Policy == "admin" {
			f.fail(t, f.user, method, path, map[string]any{}, platform.CodeFail)
		}
	}
	if databaseDigest(t, f) != before {
		t.Fatal("denied access matrix changed persisted state")
	}
	f.fail(t, f.admin, "POST", "/admin/articles", []byte(`{"broken":`), platform.CodeInvalid)
	f.fail(t, f.admin, "GET", "/admin/articles?current=0", nil, platform.CodeInvalid)
	unknown := f.call(t, f.guest, "GET", "/not-a-route", nil)
	if unknown.status != 404 || unknown.result.Flag {
		t.Fatal("unknown route was accepted")
	}
	f.fail(t, f.guest, "GET", "/files/exports/missing.md", nil, platform.CodeFail)
	resource := rowID(t, f.app.DB, "SELECT id FROM tb_resource WHERE request_method='GET' AND url='/admin/articles'")
	execSQL(t, f.app.DB, "DELETE FROM tb_role_resource WHERE role_id=1 AND resource_id=?", resource)
	f.fail(t, f.admin, "GET", "/admin/articles", nil, platform.CodeFail)
	execSQL(t, f.app.DB, "INSERT INTO tb_role_resource(role_id,resource_id) VALUES(1,?)", resource)
	execSQL(t, f.app.DB, "INSERT INTO tb_user_role(user_id,role_id) VALUES(11,1)")
	f.ok(t, f.user, "POST", "/admin/articles", map[string]any{"articleTitle": "Owned by profile", "articleContent": "body", "categoryName": "生活记录", "tagNameList": []string{"开始"}, "type": 1, "status": 1, "userId": 999})
	if rowID(t, f.app.DB, "SELECT user_id FROM tb_article WHERE article_title='Owned by profile'") != 11 {
		t.Fatal("author trusted request or auth ID")
	}
	profile := object(t, f.ok(t, f.user, "POST", "/login", url.Values{"username": {"reader@example.test"}, "password": {"fixture-password"}}))
	raw, _ := json.Marshal(profile)
	if strings.Contains(string(raw), "password") || strings.Contains(string(raw), "fixture-password") {
		t.Fatal("credentials leaked in login DTO")
	}
	execSQL(t, f.app.DB, "DELETE FROM tb_user_role WHERE user_id=11 AND role_id=1")
	f.fail(t, f.user, "POST", "/admin/articles", map[string]any{}, platform.CodeFail)
}

func databaseDigest(t *testing.T, f *fixture) [32]byte {
	t.Helper()
	rows, err := f.app.DB.Query("SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'tb_%' ORDER BY name")
	must(t, err)
	var tables []string
	for rows.Next() {
		var name string
		must(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	must(t, rows.Err())
	rows.Close()
	var data bytes.Buffer
	for _, table := range tables {
		data.WriteString(table)
		rows, err := f.app.DB.Query("SELECT * FROM " + table + " ORDER BY rowid")
		must(t, err)
		columns, err := rows.Columns()
		must(t, err)
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(values))
			for i := range values {
				pointers[i] = &values[i]
			}
			must(t, rows.Scan(pointers...))
			encoded, err := json.Marshal(values)
			must(t, err)
			data.Write(encoded)
		}
		must(t, rows.Err())
		rows.Close()
	}
	return sha256.Sum256(data.Bytes())
}

func TestControlledVerificationExpiryRecoveryAndAtomicConsumption(t *testing.T) {
	p := newProviders(t)
	f := newFixture(t, p.configure)
	var ticks atomic.Int64
	ticks.Store(time.Now().UnixNano())
	clock := func() time.Time { return time.Unix(0, ticks.Load()) }
	cfg := f.cfg
	service := identity.New(f.app.DB, cfg, files.New(f.app.DB, cfg, nil, clock), mail.New(f.app.DB, cfg, nil, clock), social.New(f.app.DB, cfg, nil, clock), site.New(f.app.DB, cfg, clock), analytics.New(f.app.DB, cfg, nil, clock), engagement.New(f.app.DB, cfg, clock), clock)
	engine, err := platform.NewRouter(service.Routes(), platform.Callbacks{Resolve: service.Resolve, Authorize: service.Authorize}, cfg)
	must(t, err)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	timed := &fixture{app: f.app, cfg: cfg, server: server, hits: make(map[string]int), guest: newClient(t), user: newClient(t)}
	timed.cfg.PublicURL = server.URL
	requestCode := func(email string) string {
		t.Helper()
		ticks.Add(int64(time.Minute + time.Second))
		timed.ok(t, timed.guest, "GET", "/users/code?username="+url.QueryEscape(email), nil)
		return p.code(t)
	}
	email := "clocked@example.test"
	code := requestCode(email)
	timed.ok(t, timed.guest, "POST", "/register", map[string]any{"username": email, "password": "clocked-password", "code": code})
	timed.ok(t, timed.user, "POST", "/login", url.Values{"username": {email}, "password": {"clocked-password"}})
	code = requestCode(email)
	payload, _ := json.Marshal(map[string]any{"username": email, "password": "replacement-password", "code": code})
	type outcome struct {
		result platform.Result
		err    error
	}
	results := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r, err := http.NewRequest("PUT", server.URL+"/users/password", bytes.NewReader(payload))
			if err != nil {
				results <- outcome{err: err}
				return
			}
			r.Header.Set("Content-Type", "application/json")
			response, err := timed.user.Do(r)
			if err != nil {
				results <- outcome{err: err}
				return
			}
			defer response.Body.Close()
			var result platform.Result
			err = json.NewDecoder(response.Body).Decode(&result)
			results <- outcome{result, err}
		}()
	}
	success := 0
	for i := 0; i < 2; i++ {
		result := <-results
		must(t, result.err)
		if result.result.Flag {
			success++
		} else if result.result.Code != platform.CodeInvalid {
			t.Fatalf("unexpected concurrent code result: %#v", result.result)
		}
	}
	if success != 1 {
		t.Fatalf("verification code consumed successfully %d times", success)
	}
	timed.fail(t, timed.user, "PUT", "/users/info", map[string]any{"nickname": "should fail"}, platform.CodeFail)
	timed.ok(t, timed.user, "POST", "/login", url.Values{"username": {email}, "password": {"replacement-password"}})
	code = requestCode("bound@example.test")
	timed.ok(t, timed.user, "POST", "/users/email", map[string]any{"email": "BOUND@example.test", "code": code})
	uid := rowID(t, f.app.DB, "SELECT user_info_id FROM tb_user_auth WHERE username=?", email)
	if rowID(t, f.app.DB, "SELECT count(*) FROM tb_user_info WHERE id=? AND email='bound@example.test'", uid) != 1 {
		t.Fatal("verified email binding missing")
	}
	code = requestCode("expired@example.test")
	ticks.Add(int64(15 * time.Minute))
	timed.fail(t, timed.guest, "POST", "/register", map[string]any{"username": "expired@example.test", "password": "valid-password", "code": code}, platform.CodeInvalid)
	code = requestCode("missing@example.test")
	timed.fail(t, timed.guest, "PUT", "/users/password", map[string]any{"username": "missing@example.test", "password": "valid-password", "code": code}, platform.CodeMissing)
	timed.ok(t, timed.guest, "POST", "/register", map[string]any{"username": "missing@example.test", "password": "valid-password", "code": code})
	p.reject.Store(true)
	ticks.Add(int64(time.Minute + time.Second))
	timed.fail(t, timed.guest, "GET", "/users/code?username=rejected%40example.test", nil, platform.CodeFail)
	code = p.code(t)
	p.reject.Store(false)
	timed.fail(t, timed.guest, "POST", "/register", map[string]any{"username": "rejected@example.test", "password": "valid-password", "code": code}, platform.CodeInvalid)
	if rowID(t, f.app.DB, "SELECT count(*) FROM tb_mail_delivery WHERE status='failed'") != 1 {
		t.Fatal("controlled SMTP refusal was not recorded")
	}
}
