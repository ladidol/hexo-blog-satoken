package site_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"blog-go/internal/analytics"
	"blog-go/internal/article"
	"blog-go/internal/bootstrap"
	"blog-go/internal/catalog"
	"blog-go/internal/chat"
	"blog-go/internal/community"
	"blog-go/internal/engagement"
	"blog-go/internal/identity"
	"blog-go/internal/mail"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
)

var ctx = context.Background()
var fixedTime = time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)

func fixture(t *testing.T) (*site.Service, *sql.DB, platform.Config) {
	t.Helper()
	cfg := platform.Config{DataDir: t.TempDir(), PublicURL: "http://localhost:8888", FrontendURL: "http://localhost:8080", TimeZone: time.FixedZone("Asia/Shanghai", 8*3600), Values: map[string]string{"BLOG_SMTP_PASSWORD": "private-credential"}}
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrate(t, db, cfg)
	return site.New(db, cfg, func() time.Time { return fixedTime }), db, cfg
}

func migrate(t *testing.T, db *sql.DB, cfg platform.Config) {
	t.Helper()
	err := platform.Migrate(ctx, db, []string{identity.Schema, catalog.Schema, article.Schema, community.Schema, media.Schema, site.Schema, analytics.Schema, mail.Schema, chat.Schema, engagement.Schema}, func(ctx context.Context, tx *sql.Tx) error { return bootstrap.Seed(ctx, tx, cfg) })
	if err != nil {
		t.Fatal(err)
	}
}

func input(value any) platform.Input {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return platform.Input{Body: data}
}

func object(t *testing.T, value any) platform.Object {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out platform.Object
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func config(t *testing.T, s *site.Service) platform.Object {
	t.Helper()
	value, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return object(t, value)
}

func pages(t *testing.T, s *site.Service) []platform.Object {
	t.Helper()
	value, err := s.ListPages(ctx, platform.Input{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out []platform.Object
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("page list must be an array")
	}
	return out
}

func requireCode(t *testing.T, err error, code int) {
	t.Helper()
	var business *platform.Error
	if !errors.As(err, &business) || business.Code != code {
		t.Fatalf("error=%v, want code %d", err, code)
	}
}

func execute(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestSiteInitialHomeUsesPersistedPublicCountsAndProtocol(t *testing.T) {
	s, db, _ := fixture(t)
	w := config(t, s)
	if len(w) != 24 || w["websiteName"] != "本地博客" || w["websiteAuthor"] != "本地开发者" || w["websocketUrl"] != "ws://localhost:8888/websocket" {
		t.Fatalf("incomplete website: %v", w)
	}
	for key, want := range map[string]float64{"isCommentReview": 1, "isMessageReview": 1, "isEmailNotice": 0, "isReward": 0, "isChatRoom": 1, "isMusicPlayer": 0} {
		if w[key] != want {
			t.Fatalf("%s=%v", key, w[key])
		}
	}
	for _, key := range []string{"websiteAvatar", "touristAvatar", "userAvatar", "articleCover"} {
		if !strings.HasPrefix(w[key].(string), "http://localhost:8888/files/defaults/") {
			t.Fatalf("unusable local asset: %v", w[key])
		}
	}
	if !reflect.DeepEqual(w["socialLoginList"], []any{"qq", "weibo"}) || !reflect.DeepEqual(w["socialUrlList"], []any{}) {
		t.Fatal("social arrays changed")
	}
	if _, err := time.Parse("2006-01-02", w["websiteCreateTime"].(string)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"websiteIntro", "websiteNotice", "websiteRecordNo", "github", "gitee", "qq", "weiXinQRCode", "alipayQRCode"} {
		if w[key] != "" {
			t.Fatalf("unexpected default %s=%v", key, w[key])
		}
	}
	for id, state := range []struct{ status, deleted int }{{1, 0}, {1, 1}, {2, 0}, {3, 0}} {
		execute(t, db, "INSERT INTO tb_article(id,user_id,article_title,article_content,status,is_delete,create_time) VALUES(?,1,'article','body',?,?,?)", id+2, state.status, state.deleted, "2026-09-10T16:30:00")
	}
	execute(t, db, "INSERT INTO tb_category(category_name,create_time) VALUES('empty category','2026-09-10T16:30:00')")
	execute(t, db, "INSERT INTO tb_tag(tag_name,create_time) VALUES('empty tag','2026-09-10T16:30:00')")
	execute(t, db, "INSERT INTO tb_visit_record(visit_date,visitor_key,create_time) VALUES('2026-09-09','visitor','2026-09-09T10:00:00'),('2026-09-10','visitor','2026-09-10T10:00:00'),('2026-09-10','other','2026-09-10T11:00:00')")
	// The durable visitor facts remain authoritative even if the aggregate needs rebuilding.
	execute(t, db, "INSERT INTO tb_unique_view(views_count,create_time) VALUES(99,'2026-09-10T00:00:00')")
	value, err := s.GetBlogHomeInfo(ctx, platform.Input{})
	if err != nil {
		t.Fatal(err)
	}
	home := object(t, value)
	if len(home) != 6 || home["articleCount"] != float64(2) || home["categoryCount"] != float64(2) || home["tagCount"] != float64(2) || home["viewsCount"] != "3" {
		t.Fatalf("incorrect home counts: %v", home)
	}
	if !reflect.DeepEqual(home["websiteConfig"], map[string]any(w)) || len(home["pageList"].([]any)) == 0 {
		t.Fatal("home does not compose real config and pages")
	}
	for _, p := range pages(t, s) {
		if len(p) != 4 {
			t.Fatalf("internal page columns escaped: %v", p)
		}
	}
	if data, _ := json.Marshal(home); strings.Contains(string(data), "private-credential") {
		t.Fatal("runtime credentials escaped")
	}
}

func TestSiteConfigValidationAtomicWritesAndRestart(t *testing.T) {
	s, db, cfg := fixture(t)
	updated := config(t, s)
	updated["websiteName"], updated["websiteNotice"], updated["qq"] = "更新后的博客", "新的公告", "12345678"
	updated["github"], updated["websocketUrl"] = "https://github.com/example", "wss://example.test/websocket"
	updated["socialLoginList"], updated["socialUrlList"] = []string{}, []string{"github", "qq"}
	updated["isCommentReview"], updated["isMessageReview"] = 0, 0
	if result, err := s.UpdateWebsiteConfig(ctx, input(updated)); err != nil || result != nil {
		t.Fatalf("save: %v %v", result, err)
	}
	want := object(t, updated)
	if got := config(t, site.New(db, cfg, nil)); !reflect.DeepEqual(got, want) {
		t.Fatalf("shared config stale: %v", got)
	}
	admin, err := s.GetWebsiteConfig(ctx, platform.Input{})
	if err != nil || !reflect.DeepEqual(object(t, admin), want) {
		t.Fatalf("admin config mismatch: %v", err)
	}
	for _, bad := range []string{`null`, `[]`, `{}`, `{"isCommentReview":2}`, `{"isMessageReview":-1}`, `{"isEmailNotice":true}`, `{"isReward":"1"}`, `{"isChatRoom":0.5}`, `{"isMusicPlayer":null}`, `{"websiteName":3}`, `{"websiteAuthor":null}`, `{"github":"javascript:alert(1)"}`, `{"gitee":"//example.test/path"}`, `{"websiteAvatar":"file:///tmp/a"}`, `{"articleCover":"data:image/svg+xml,bad"}`, `{"websocketUrl":"https://example.test/websocket"}`, `{"touristAvatar":"https://user:pass@example.test/a"}`, `{"userAvatar":"http:///missing-host"}`, `{"socialLoginList":[1]}`, `{"socialUrlList":"github"}`, `{"BLOG_SMTP_PASSWORD":"injection"}`, `{"config":{"password":"injection"}}`, `{"websiteName":"partial","isCommentReview":4}`} {
		t.Run(bad, func(t *testing.T) {
			_, err := s.UpdateWebsiteConfig(ctx, platform.Input{Body: []byte(bad)})
			requireCode(t, err, platform.CodeInvalid)
			if !reflect.DeepEqual(config(t, s), want) {
				t.Fatal("rejected config partially persisted")
			}
		})
	}
	// Returned values have caller ownership, not a shared mutable cache.
	caller, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	caller["websiteName"] = "unpersisted mutation"
	if config(t, s)["websiteName"] != want["websiteName"] {
		t.Fatal("caller mutated shared config")
	}
	if _, err := s.UpdateWebsiteConfig(ctx, input(platform.Object{"socialLoginList": nil, "socialUrlList": nil})); err != nil {
		t.Fatal(err)
	}
	want["socialLoginList"], want["socialUrlList"] = []any{}, []any{}
	if !reflect.DeepEqual(config(t, s), want) {
		t.Fatal("null arrays were not normalized or partial update erased fields")
	}
	var stamp string
	if err := db.QueryRow("SELECT update_time FROM tb_website_config WHERE id=1").Scan(&stamp); err != nil || stamp != "2026-09-10T16:30:00" {
		t.Fatalf("update timestamp %s %v", stamp, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	migrate(t, reopened, cfg)
	if got := config(t, site.New(reopened, cfg, nil)); !reflect.DeepEqual(got, want) {
		t.Fatalf("restart lost website: %v", got)
	}
}

func TestSiteAboutAndPageCRUDPersistAndKeepOriginalShapes(t *testing.T) {
	s, db, cfg := fixture(t)
	execute(t, db, "DELETE FROM tb_about")
	if value, err := s.GetAbout(ctx, platform.Input{}); err != nil || value != "" {
		t.Fatalf("empty about: %v %v", value, err)
	}
	for _, body := range []string{"# 更新\n\n正文与 <b>HTML</b>", ""} {
		if result, err := s.UpdateAbout(ctx, input(platform.Object{"aboutContent": body})); err != nil || result != nil {
			t.Fatalf("save about: %v", err)
		}
		got, err := s.GetAbout(ctx, platform.Input{})
		if err != nil || got != body {
			t.Fatalf("about=%v %v", got, err)
		}
	}
	for _, body := range []string{`{}`, `{"aboutContent":null}`, `{"aboutContent":5}`, `[]`} {
		_, err := s.UpdateAbout(ctx, platform.Input{Body: []byte(body)})
		requireCode(t, err, platform.CodeInvalid)
	}
	content := "最终正文\n第二行"
	if _, err := s.UpdateAbout(ctx, input(platform.Object{"aboutContent": content})); err != nil {
		t.Fatal(err)
	}
	p := platform.Object{"id": nil, "pageName": "测试页面", "pageLabel": "custom", "pageCover": "https://example.test/cover.png"}
	before := len(pages(t, s))
	if result, err := s.SaveOrUpdatePage(ctx, input(p)); err != nil || result != nil {
		t.Fatalf("create: %v %v", result, err)
	}
	list := pages(t, s)
	if len(list) != before+1 {
		t.Fatal("page was not inserted")
	}
	p = list[len(list)-1]
	p["pageName"], p["pageCover"] = "改名", "http://localhost:8888/files/new.png"
	if _, err := s.SaveOrUpdatePage(ctx, input(p)); err != nil {
		t.Fatal(err)
	}
	if list = pages(t, s); len(list) != before+1 || !reflect.DeepEqual(list[len(list)-1], p) {
		t.Fatal("update inserted duplicate or did not persist")
	}
	conflict := platform.Object{"pageName": "conflict", "pageLabel": "custom", "pageCover": "https://example.test/cover.png"}
	_, err := s.SaveOrUpdatePage(ctx, input(conflict))
	requireCode(t, err, platform.CodeFail)
	conflict["id"] = p["id"]
	conflict["pageLabel"] = list[0]["pageLabel"]
	_, err = s.SaveOrUpdatePage(ctx, input(conflict))
	requireCode(t, err, platform.CodeFail)
	if got := pages(t, s); !reflect.DeepEqual(got, list) {
		t.Fatal("conflicting update partially changed page data")
	}
	for _, bad := range []string{`{}`, `{"pageName":" ","pageLabel":"x","pageCover":"https://example.test/a"}`, `{"pageName":"x","pageLabel":"","pageCover":"https://example.test/a"}`, `{"pageName":"x","pageLabel":"x","pageCover":"javascript:bad"}`, `{"id":0,"pageName":"x","pageLabel":"x","pageCover":"https://example.test/a"}`, `{"id":1.2,"pageName":"x","pageLabel":"x","pageCover":"https://example.test/a"}`} {
		_, err := s.SaveOrUpdatePage(ctx, platform.Input{Body: []byte(bad)})
		requireCode(t, err, platform.CodeInvalid)
	}
	// saveOrUpdate also preserves the original create-with-explicit-id behavior.
	if _, err := s.SaveOrUpdatePage(ctx, input(platform.Object{"id": 500, "pageName": "delete me", "pageLabel": "delete", "pageCover": "https://example.test/a"})); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if result, err := s.DeletePage(ctx, platform.Input{Params: map[string]string{"pageId": "500"}}); err != nil || result != nil {
			t.Fatalf("idempotent delete: %v", err)
		}
	}
	for _, id := range []string{"", "0", "-1", "1 or 1=1", "1.2"} {
		_, err := s.DeletePage(ctx, platform.Input{Params: map[string]string{"pageId": id}})
		requireCode(t, err, platform.CodeInvalid)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	migrate(t, reopened, cfg)
	s = site.New(reopened, cfg, nil)
	if got, err := s.GetAbout(ctx, platform.Input{}); err != nil || got != content {
		t.Fatalf("about lost on restart: %v %v", got, err)
	}
	if got := pages(t, s); !reflect.DeepEqual(got, list) {
		t.Fatalf("pages changed on restart: %v", got)
	}
	execute(t, reopened, "DELETE FROM tb_page")
	if got := pages(t, s); len(got) != 0 {
		t.Fatal("empty list is not empty")
	}
	value, err := s.GetBlogHomeInfo(ctx, platform.Input{})
	if err != nil || !reflect.DeepEqual(object(t, value)["pageList"], []any{}) {
		t.Fatal("home empty pageList is not []")
	}
}

func TestSiteConfigRejectsNullListMembers(t *testing.T) {
	s, _, _ := fixture(t)
	want := config(t, s)
	for _, body := range []string{`{"socialLoginList":[null]}`, `{"socialUrlList":["github",null]}`} {
		_, err := s.UpdateWebsiteConfig(ctx, platform.Input{Body: []byte(body)})
		requireCode(t, err, platform.CodeInvalid)
		if !reflect.DeepEqual(config(t, s), want) {
			t.Fatal("invalid list members persisted")
		}
	}
}

func TestSiteConcurrentPartialConfigAndUniquePages(t *testing.T) {
	s, db, cfg := fixture(t)
	other := site.New(db, cfg, nil)
	var wg sync.WaitGroup
	errorsOut := make(chan error, 2)
	for index, service := range []*site.Service{s, other} {
		wg.Add(1)
		go func(index int, service *site.Service) {
			defer wg.Done()
			key := []string{"websiteName", "websiteNotice"}[index]
			for n := 0; n < 15; n++ {
				if _, err := service.UpdateWebsiteConfig(ctx, input(platform.Object{key: fmt.Sprintf("value-%d", n)})); err != nil {
					errorsOut <- err
					return
				}
			}
		}(index, service)
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		t.Fatal(err)
	}
	w := config(t, s)
	if w["websiteName"] != "value-14" || w["websiteNotice"] != "value-14" {
		t.Fatalf("concurrent writes lost fields: %v", w)
	}
	errorsOut = make(chan error, 2)
	for _, service := range []*site.Service{s, other} {
		wg.Add(1)
		go func(service *site.Service) {
			defer wg.Done()
			_, err := service.SaveOrUpdatePage(ctx, input(platform.Object{"pageName": "race", "pageLabel": "race", "pageCover": "https://example.test/a"}))
			errorsOut <- err
		}(service)
	}
	wg.Wait()
	close(errorsOut)
	successes := 0
	for err := range errorsOut {
		if err == nil {
			successes++
		} else {
			requireCode(t, err, platform.CodeFail)
		}
	}
	if successes != 1 {
		t.Fatalf("unique page writes succeeded %d times", successes)
	}
}

func TestSiteReadAndWriteFailuresRemainFailures(t *testing.T) {
	s, db, _ := fixture(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for name, call := range map[string]func(context.Context) error{
		"config": func(ctx context.Context) error { _, err := s.Config(ctx); return err },
		"home":   func(ctx context.Context) error { _, err := s.GetBlogHomeInfo(ctx, platform.Input{}); return err },
		"config write": func(ctx context.Context) error {
			_, err := s.UpdateWebsiteConfig(ctx, input(platform.Object{"websiteName": "cancelled"}))
			return err
		},
		"about": func(ctx context.Context) error { _, err := s.GetAbout(ctx, platform.Input{}); return err },
		"about write": func(ctx context.Context) error {
			_, err := s.UpdateAbout(ctx, input(platform.Object{"aboutContent": "cancelled"}))
			return err
		},
		"pages": func(ctx context.Context) error { _, err := s.ListPages(ctx, platform.Input{}); return err },
		"page write": func(ctx context.Context) error {
			_, err := s.SaveOrUpdatePage(ctx, input(platform.Object{"pageName": "cancelled", "pageLabel": "cancelled", "pageCover": "https://example.test/a"}))
			return err
		},
		"page delete": func(ctx context.Context) error {
			_, err := s.DeletePage(ctx, platform.Input{Params: map[string]string{"pageId": "1"}})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel identity lost: %v", err)
			}
		})
	}
	if config(t, s)["websiteName"] != "本地博客" {
		t.Fatal("cancelled write persisted")
	}
	execute(t, db, "UPDATE tb_website_config SET config='invalid json' WHERE id=1")
	if _, err := s.Config(ctx); err == nil {
		t.Fatal("corrupt config returned defaults")
	}
	if _, err := s.GetBlogHomeInfo(ctx, platform.Input{}); err == nil {
		t.Fatal("home hid config failure")
	}
	execute(t, db, "DELETE FROM tb_website_config WHERE id=1")
	if _, err := s.Config(ctx); err == nil {
		t.Fatal("missing config returned synthetic success")
	}
	if _, err := s.UpdateWebsiteConfig(ctx, input(platform.Object{"websiteName": "missing"})); err == nil {
		t.Fatal("missing config update reported success")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, call := range []platform.Endpoint{s.GetAbout, s.ListPages, s.GetWebsiteConfig, s.GetBlogHomeInfo} {
		if _, err := call(ctx, platform.Input{}); err == nil {
			t.Fatal("closed DB read returned success")
		}
	}
	if _, err := s.UpdateAbout(ctx, input(platform.Object{"aboutContent": "failed"})); err == nil {
		t.Fatal("closed DB write returned success")
	}
	if _, err := s.SaveOrUpdatePage(ctx, input(platform.Object{"pageName": "failed", "pageLabel": "failed", "pageCover": "https://example.test/a"})); err == nil {
		t.Fatal("closed DB page write returned success")
	}
	if _, err := s.DeletePage(ctx, platform.Input{Params: map[string]string{"pageId": "1"}}); err == nil {
		t.Fatal("closed DB delete returned success")
	}
}

func TestSiteRoutesUseCanonicalMetadataAndAuthorization(t *testing.T) {
	s, _, cfg := fixture(t)
	routes := s.Routes()
	if len(routes) != len(site.RouteSpecs) {
		t.Fatal("missing route")
	}
	for i, route := range routes {
		if route.Spec != site.RouteSpecs[i] || route.Handle == nil || route.Raw != nil {
			t.Fatalf("incorrect route: %+v", route.Spec)
		}
	}
	loggedIn := false
	allow := false
	audits := []string{}
	engine, err := platform.NewRouter(routes, platform.Callbacks{
		Resolve: func(*http.Request) (platform.Actor, error) {
			return platform.Actor{AuthID: 1, UserInfoID: 1, LoggedIn: loggedIn}, nil
		},
		Authorize: func(context.Context, platform.Actor, string, string) error {
			if !allow {
				return &platform.Error{Code: platform.CodeFail, Message: "denied"}
			}
			return nil
		},
		Audit: func(_ context.Context, event platform.AuditEvent) error {
			audits = append(audits, event.Route.Operation)
			return nil
		},
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string, want int) platform.Object {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		out := platform.Object{}
		if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out["code"] != float64(want) {
			t.Fatalf("%s %s: %s", method, path, response.Body)
		}
		return out
	}
	call("GET", "/", "", platform.CodeOK)
	call("GET", "/about", "", platform.CodeOK)
	call("PUT", "/admin/about", `{"aboutContent":"not authorized"}`, platform.CodeFail)
	loggedIn = true
	call("PUT", "/admin/about", `{"aboutContent":"not authorized"}`, platform.CodeFail)
	if value, err := s.GetAbout(ctx, platform.Input{}); err != nil || value == "not authorized" {
		t.Fatal("unauthorized write reached service")
	}
	allow = true
	call("GET", "/admin/website/config", "", platform.CodeOK)
	call("PUT", "/admin/website/config", `{"websiteNotice":"route notice"}`, platform.CodeOK)
	if config(t, s)["websiteNotice"] != "route notice" {
		t.Fatal("config route misbound")
	}
	call("PUT", "/admin/about", `{"aboutContent":"route about"}`, platform.CodeOK)
	if out := call("GET", "/about", "", platform.CodeOK); out["data"] != "route about" {
		t.Fatal("about route misbound")
	}
	call("POST", "/admin/pages", `{"id":700,"pageName":"route page","pageLabel":"route","pageCover":"https://example.test/a"}`, platform.CodeOK)
	out := call("GET", "/admin/pages", "", platform.CodeOK)
	if list := out["data"].([]any); list[len(list)-1].(map[string]any)["id"] != float64(700) {
		t.Fatal("page route misbound")
	}
	call("DELETE", "/admin/pages/700", "", platform.CodeOK)
	if got := pages(t, s); got[len(got)-1]["id"] == float64(700) {
		t.Fatal("delete route misbound")
	}
	if !reflect.DeepEqual(audits, []string{"UpdateAbout", "SaveOrUpdatePage", "DeletePage"}) {
		t.Fatalf("audit metadata changed: %v", audits)
	}
}
