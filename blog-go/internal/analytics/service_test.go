package analytics_test

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"blog-go/internal/analytics"
	"blog-go/internal/article"
	"blog-go/internal/catalog"
	"blog-go/internal/community"
	"blog-go/internal/engagement"
	"blog-go/internal/identity"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
)

var background = context.Background()

func setup(t *testing.T) (*sql.DB, platform.Config) {
	t.Helper()
	cfg := platform.Config{DataDir: t.TempDir(), TimeZone: time.UTC}
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := platform.Migrate(background, db, []string{identity.Schema, catalog.Schema, article.Schema, community.Schema, engagement.Schema, analytics.Schema}, nil); err != nil {
		t.Fatal(err)
	}
	return db, cfg
}
func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func scalar(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func object(t *testing.T, value any, err error) map[string]any {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	raw, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	var got map[string]any
	if e = json.Unmarshal(raw, &got); e != nil {
		t.Fatal(e)
	}
	return got
}
func dashboard(t *testing.T, s *analytics.Service) map[string]any {
	t.Helper()
	v, e := s.GetBlogBackInfo(background, platform.Input{})
	return object(t, v, e)
}
func logPage(t *testing.T, s *analytics.Service, q url.Values) map[string]any {
	t.Helper()
	v, e := s.ListOperationLogs(background, platform.Input{Query: q})
	return object(t, v, e)
}
func areas(t *testing.T, s *analytics.Service, kind string) []platform.Object {
	t.Helper()
	v, e := s.ListUserAreas(background, platform.Input{Query: url.Values{"type": {kind}}})
	if e != nil {
		t.Fatal(e)
	}
	return v.([]platform.Object)
}
func instant(value string) time.Time {
	v, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return v
}
func seedArticles(t *testing.T, db *sql.DB) {
	execSQL(t, db, `INSERT INTO tb_user_info(id,nickname,create_time) VALUES(1,'管理员','2026-09-01T00:00:00'),(2,'访客','2026-09-02T00:00:00');
 INSERT INTO tb_category(id,category_name,create_time) VALUES(1,'Go','2026-09-01T00:00:00'),(2,'Empty','2026-09-01T00:00:00');
 INSERT INTO tb_tag(id,tag_name,create_time) VALUES(1,'SQLite','2026-09-01T00:00:00');
 INSERT INTO tb_article(id,user_id,category_id,article_title,article_content,status,is_delete,create_time) VALUES
 (1,1,1,'Published','body',1,0,'2026-09-01T00:00:00'),(2,1,1,'Private','body',2,0,'2026-09-01T00:00:00'),(3,1,1,'Deleted','body',1,1,'2026-09-02T00:00:00');
 INSERT INTO tb_article_view(article_id,visitor_key,create_time) VALUES(1,'a','2026-09-01T00:00:00'),(2,'a','2026-09-01T00:00:00'),(2,'b','2026-09-01T00:00:00');
 INSERT INTO tb_message(nickname,avatar,message_content,ip_address,ip_source,is_review,create_time) VALUES('guest','','message','127.0.0.1','',0,'2026-09-01T00:00:00')`)
}

func TestReportDailyIdentityPersistenceAndReconcile(t *testing.T) {
	db, cfg := setup(t)
	now := instant("2026-09-09T15:59:59Z")
	s := analytics.New(db, cfg, nil, func() time.Time { return now })
	in := platform.Input{IP: "203.0.113.4", Browser: "Chrome", OS: "Windows 10"}
	for i := 0; i < 3; i++ {
		if _, err := s.Report(background, in); err != nil {
			t.Fatal(err)
		}
	}
	sum := md5.Sum([]byte(in.IP + in.Browser + in.OS))
	if got := scalar(t, db, `SELECT count(*) FROM tb_visit_record WHERE visit_date='2026-09-09' AND visitor_key=?`, hex.EncodeToString(sum[:])); got != 1 {
		t.Fatal("visitor hash/date mismatch", got)
	}
	// Midnight in Shanghai is independent of a supplied UTC configuration.
	now = now.Add(2 * time.Second)
	var wg sync.WaitGroup
	failures := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Report(background, in); failures <- err }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	in.Browser = "Firefox"
	if _, err := s.Report(background, in); err != nil {
		t.Fatal(err)
	}
	if scalar(t, db, "SELECT count(*) FROM tb_visit_record") != 3 {
		t.Fatal("daily idempotency broken")
	}
	if scalar(t, db, `SELECT views_count FROM tb_unique_view WHERE substr(create_time,1,10)='2026-09-10'`) != 2 {
		t.Fatal("report failed to upsert aggregate")
	}
	execSQL(t, db, `UPDATE tb_unique_view SET views_count=999; DELETE FROM tb_unique_view WHERE substr(create_time,1,10)='2026-09-09'`)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	now = instant("2026-09-13T10:00:00Z")
	s = analytics.New(reopened, cfg, nil, func() time.Time { return now })
	for i := 0; i < 2; i++ {
		if err := s.ReconcileDays(background, now); err != nil {
			t.Fatal(err)
		}
	}
	if scalar(t, reopened, "SELECT sum(views_count) FROM tb_unique_view") != 3 || scalar(t, reopened, "SELECT count(*) FROM tb_visit_record") != 3 {
		t.Fatal("reconcile lost/duplicated persistent facts")
	}
	got := dashboard(t, s)
	if got["viewsCount"] != float64(3) {
		t.Fatal(got)
	}
	days := got["uniqueViewDTOList"].([]any)
	if len(days) != 8 || days[0].(map[string]any)["day"] != "2026-09-06" || days[7].(map[string]any)["day"] != "2026-09-13" {
		t.Fatal(days)
	}
	if days[3].(map[string]any)["viewsCount"] != float64(1) || days[4].(map[string]any)["viewsCount"] != float64(2) || days[7].(map[string]any)["viewsCount"] != float64(0) {
		t.Fatal(days)
	}
}

func TestReportAndReconcileRollbackAndCancellation(t *testing.T) {
	db, cfg := setup(t)
	now := instant("2026-09-10T00:00:00Z")
	s := analytics.New(db, cfg, nil, func() time.Time { return now })
	execSQL(t, db, `CREATE TRIGGER reject_uv BEFORE INSERT ON tb_unique_view BEGIN SELECT RAISE(ABORT,'aggregate rejected'); END`)
	if _, err := s.Report(background, platform.Input{IP: "127.0.0.1", Browser: "Chrome", OS: "Linux"}); err == nil {
		t.Fatal("ignored aggregate failure")
	}
	if scalar(t, db, "SELECT count(*) FROM tb_visit_record") != 0 {
		t.Fatal("partial visitor committed")
	}
	execSQL(t, db, `DROP TRIGGER reject_uv; INSERT INTO tb_unique_view(views_count,create_time) VALUES(99,'2026-09-09T00:00:00'); INSERT INTO tb_visit_record VALUES('2026-09-08','key','','2026-09-08T00:00:00'); CREATE TRIGGER reject_uv BEFORE INSERT ON tb_unique_view BEGIN SELECT RAISE(ABORT,'aggregate rejected'); END`)
	if err := s.ReconcileDays(background, now); err == nil {
		t.Fatal("ignored repair failure")
	}
	if scalar(t, db, "SELECT views_count FROM tb_unique_view") != 99 {
		t.Fatal("partial reconciliation committed")
	}
	ctx, cancel := context.WithCancel(background)
	cancel()
	if _, err := s.Report(ctx, platform.Input{IP: "127.0.0.1"}); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
	if err := s.ReconcileDays(ctx, now); !errors.Is(err, context.Canceled) {
		t.Fatal("lost reconciliation cancellation", err)
	}
	if _, err := s.GetBlogBackInfo(ctx, platform.Input{}); !errors.Is(err, context.Canceled) {
		t.Fatal("lost read cancellation", err)
	}
}

func TestDashboardCountsProjectionsAndEmptyLists(t *testing.T) {
	db, cfg := setup(t)
	s := analytics.New(db, cfg, nil, func() time.Time { return instant("2026-09-10T00:00:00Z") })
	empty := dashboard(t, s)
	for _, name := range []string{"categoryDTOList", "tagDTOList", "articleStatisticsList", "articleRankDTOList"} {
		if list, ok := empty[name].([]any); !ok || len(list) != 0 {
			t.Fatalf("%s: %v", name, empty[name])
		}
	}
	seedArticles(t, db)
	got := dashboard(t, s)
	for name, want := range map[string]float64{"viewsCount": 0, "messageCount": 1, "userCount": 2, "articleCount": 2} {
		if got[name] != want {
			t.Fatalf("%s=%v", name, got[name])
		}
	}
	categories := got["categoryDTOList"].([]any)
	if len(categories) != 2 || categories[0].(map[string]any)["articleCount"] != float64(1) || categories[1].(map[string]any)["articleCount"] != float64(0) {
		t.Fatal(categories)
	}
	stats := got["articleStatisticsList"].([]any)
	if len(stats) != 2 || stats[0].(map[string]any)["count"] != float64(2) || stats[1].(map[string]any)["count"] != float64(1) {
		t.Fatal(stats)
	}
	ranks := got["articleRankDTOList"].([]any)
	if len(ranks) != 2 || ranks[0].(map[string]any)["articleTitle"] != "Private" || ranks[0].(map[string]any)["viewsCount"] != float64(2) {
		t.Fatal(ranks)
	}
	tags := got["tagDTOList"].([]any)
	if len(tags) != 1 || len(tags[0].(map[string]any)) != 2 || tags[0].(map[string]any)["tagName"] != "SQLite" {
		t.Fatal(tags)
	}
}

func TestAuditWhitelistSummariesFilteringAndDeletion(t *testing.T) {
	db, cfg := setup(t)
	seedArticles(t, db)
	now := instant("2026-09-10T00:00:00Z")
	s := analytics.New(db, cfg, nil, func() time.Time { return now })
	actor := platform.Actor{AuthID: 7, UserInfoID: 1, LoggedIn: true, Nickname: "管理员"}
	specs := append([]platform.RouteSpec{}, article.RouteSpecs...)
	for _, list := range [][]platform.RouteSpec{catalog.RouteSpecs, community.RouteSpecs, identity.RouteSpecs, media.RouteSpecs, site.RouteSpecs, analytics.RouteSpecs} {
		specs = append(specs, list...)
	}
	expected := 0
	for _, spec := range specs {
		if err := s.Audit(background, platform.AuditEvent{Actor: actor, Route: spec, ObjectIDs: []int64{1, 2}, Count: 2, Code: platform.CodeOK, IP: "127.0.0.1", At: now}); err != nil {
			t.Fatal(err)
		}
		if spec.Audit {
			expected++
		}
	}
	if scalar(t, db, "SELECT count(*) FROM tb_operation_log") != int64(expected) || expected != 28 {
		t.Fatal("audit does not match controller allowlist", expected)
	}
	for _, change := range []func(*platform.AuditEvent){func(e *platform.AuditEvent) { e.Code = platform.CodeFail }, func(e *platform.AuditEvent) { e.Actor.LoggedIn = false }, func(e *platform.AuditEvent) { e.Route.Path = "/admin/unknown?token=secret" }, func(e *platform.AuditEvent) { e.Route.Operation = "secret" }, func(e *platform.AuditEvent) { e.Route.Audit = false }, func(e *platform.AuditEvent) { e.Route.Policy = "public" }} {
		event := platform.AuditEvent{Actor: actor, Route: article.RouteSpecs[3], Count: 1, Code: platform.CodeOK, At: now}
		change(&event)
		if err := s.Audit(background, event); err != nil {
			t.Fatal(err)
		}
	}
	if scalar(t, db, "SELECT count(*) FROM tb_operation_log") != int64(expected) {
		t.Fatal("logged unapproved/failed action")
	}
	page := logPage(t, s, url.Values{"keywords": {"文章"}, "current": {"1"}, "size": {"2"}})
	if page["count"] != float64(4) || len(page["recordList"].([]any)) != 2 {
		t.Fatal(page)
	}
	first := page["recordList"].([]any)[0].(map[string]any)
	if first["optDesc"] != "物理删除文章" || first["optModule"] != "文章模块" || first["createTime"] != "2026-09-10T08:00:00" {
		t.Fatal(first)
	}
	var req, res map[string]any
	if err := json.Unmarshal([]byte(first["requestParam"].(string)), &req); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(first["responseData"].(string)), &res); err != nil {
		t.Fatal(err)
	}
	if len(req) != 2 || req["count"] != float64(2) || !reflect.DeepEqual(req["objectIds"], []any{float64(1), float64(2)}) || len(res) != 1 || res["code"] != float64(platform.CodeOK) {
		t.Fatal(req, res)
	}
	page = logPage(t, s, url.Values{"keywords": {"%' OR 1=1 --"}})
	if page["count"] != float64(0) {
		t.Fatal("unsafe log filtering", page)
	}
	for _, body := range []string{"[4,0]", "null", "{}", "[1.5]", "[]"} {
		if _, err := s.DeleteOperationLogs(background, platform.Input{Body: json.RawMessage(body)}); err == nil {
			t.Fatal("accepted invalid log IDs", body)
		}
	}
	if scalar(t, db, "SELECT count(*) FROM tb_operation_log") != int64(expected) {
		t.Fatal("invalid deletion changed data")
	}
	for i := 0; i < 2; i++ {
		if _, err := s.DeleteOperationLogs(background, platform.Input{Body: json.RawMessage(`[4,4,99999]`)}); err != nil {
			t.Fatal(err)
		}
	}
	if scalar(t, db, "SELECT count(*) FROM tb_operation_log") != int64(expected-1) || scalar(t, db, "SELECT count(*) FROM tb_article") != 3 {
		t.Fatal("delete touched wrong records")
	}
	for _, q := range []url.Values{{"current": {"0"}}, {"size": {"101"}}, {"current": {"9223372036854775807"}, "size": {"100"}}} {
		if _, err := s.ListOperationLogs(background, platform.Input{Query: q}); err == nil {
			t.Fatal("accepted invalid pagination", q)
		}
	}
}

func TestAreasUseLatestAccountAndCurrentVisitorFacts(t *testing.T) {
	db, cfg := setup(t)
	seedArticles(t, db)
	s := analytics.New(db, cfg, nil, func() time.Time { return instant("2026-09-10T00:00:00Z") })
	execSQL(t, db, `INSERT INTO tb_user_auth(user_info_id,username,password,login_type,ip_source,create_time,last_login_time) VALUES
 (1,'one','hash',1,'四川省成都市','2026-09-01T00:00:00','2026-09-10T00:00:00'),(2,'two','hash',1,'北京市','2026-09-01T00:00:00','2026-09-10T00:00:00'),(2,'three','hash',2,NULL,'2026-09-01T00:00:00',NULL);
 INSERT INTO tb_visit_record VALUES('2026-09-09','a','上海市','2026-09-09T00:00:00'),('2026-09-10','a','四川省成都市','2026-09-10T00:00:00'),('2026-09-10','b','四川省绵阳市','2026-09-10T00:00:00'),('2026-09-10','c','','2026-09-10T00:00:00')`)
	list := areas(t, s, "1")
	counts := map[string]int64{}
	for _, row := range list {
		counts[row["name"].(string)] = row["count"].(int64)
	}
	if counts["四川"] != 1 || counts["北京"] != 1 || counts["未知"] != 1 || len(counts) != 3 {
		t.Fatal(counts)
	}
	visitors := areas(t, s, "2")
	counts = map[string]int64{}
	for _, row := range visitors {
		counts[row["name"].(string)] = row["count"].(int64)
	}
	if counts["四川"] != 2 || counts["未知"] != 1 || len(counts) != 2 {
		t.Fatal(counts)
	}
	execSQL(t, db, `UPDATE tb_user_auth SET ip_source='浙江省杭州市' WHERE username='one'`)
	changed := areas(t, s, "1")
	if reflect.DeepEqual(changed, list) {
		t.Fatal("stale login region")
	}
	if _, err := s.ListUserAreas(background, platform.Input{Query: url.Values{"type": {"3"}}}); err == nil {
		t.Fatal("accepted invalid region type")
	}
}

func TestAnalyticsRoutesUseTrustedAddressAndAuthorization(t *testing.T) {
	db, cfg := setup(t)
	s := analytics.New(db, cfg, nil, func() time.Time { return instant("2026-09-10T00:00:00Z") })
	routes := s.Routes()
	if len(routes) != len(analytics.RouteSpecs) {
		t.Fatal(routes)
	}
	for i, r := range routes {
		if r.Spec != analytics.RouteSpecs[i] || r.Handle == nil {
			t.Fatal(r)
		}
	}
	engine, err := platform.NewRouter(routes, platform.Callbacks{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, forwarded := range []string{"1.2.3.4", "9.8.7.6"} {
		r := httptest.NewRequest(http.MethodPost, "/report", nil)
		r.RemoteAddr = "203.0.113.9:1234"
		r.Header.Set("X-Forwarded-For", forwarded)
		r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0) Chrome/125.0.0.0 Safari/537.36")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		if !strings.Contains(w.Body.String(), `"flag":true`) {
			t.Fatal(w.Body)
		}
	}
	sum := md5.Sum([]byte("203.0.113.9ChromeWindows 10"))
	if scalar(t, db, "SELECT count(*) FROM tb_visit_record WHERE visitor_key=?", hex.EncodeToString(sum[:])) != 1 || scalar(t, db, "SELECT count(*) FROM tb_visit_record") != 1 {
		t.Fatal("trusted identity not used")
	}
	for _, route := range routes {
		if route.Spec.Policy != "admin" {
			continue
		}
		r := httptest.NewRequest(route.Spec.Method, route.Spec.Path, strings.NewReader(`[1]`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		if !strings.Contains(w.Body.String(), `"flag":false`) {
			t.Fatal("admin route exposed", route.Spec, w.Body)
		}
	}
}
