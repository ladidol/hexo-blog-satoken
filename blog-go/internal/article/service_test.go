package article_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

var ctx = context.Background()
var fixedTime = time.Date(2026, 9, 12, 3, 4, 5, 0, time.UTC)

func fixture(t *testing.T) (*article.Service, *sql.DB, platform.Config) {
	t.Helper()
	cfg := platform.Config{DataDir: t.TempDir(), PublicURL: "http://localhost:8888", FrontendURL: "http://localhost:8080", TimeZone: time.FixedZone("Asia/Shanghai", 8*3600), Values: map[string]string{"BLOG_UPLOAD_MODE": "local"}}
	db, err := platform.OpenSQLite(cfg)
	must(t, err)
	t.Cleanup(func() { db.Close() })
	must(t, platform.Migrate(ctx, db, []string{identity.Schema, catalog.Schema, article.Schema, community.Schema, media.Schema, site.Schema, analytics.Schema, mail.Schema, chat.Schema, engagement.Schema}, func(ctx context.Context, tx *sql.Tx) error { return bootstrap.Seed(ctx, tx, cfg) }))
	// Preserve the real bootstrap account/configuration while isolating article fixtures.
	for _, table := range []string{"tb_article", "tb_category", "tb_tag"} {
		_, err := db.Exec("DELETE FROM " + table)
		must(t, err)
	}
	return service(db, cfg), db, cfg
}
func service(db *sql.DB, cfg platform.Config) *article.Service {
	now := func() time.Time { return fixedTime }
	return article.New(db, cfg, catalog.New(db, cfg, now), site.New(db, cfg, now), files.New(db, cfg, nil, now), engagement.New(db, cfg, now), now)
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func input(value any) platform.Input {
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return platform.Input{Body: b, Actor: platform.Actor{LoggedIn: true, UserInfoID: 1}, VisitorKey: "browser-a"}
}
func idInput(id int64) platform.Input {
	in := input(nil)
	in.Params = map[string]string{"articleId": fmt.Sprint(id)}
	return in
}
func count(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	must(t, db.QueryRow(query, args...).Scan(&n))
	return n
}
func code(t *testing.T, err error, want int) {
	t.Helper()
	var e *platform.Error
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("error=%v, want code %d", err, want)
	}
}
func save(t *testing.T, s *article.Service, db *sql.DB, title string, status int, category string, tags []string) int64 {
	t.Helper()
	_, err := s.SaveOrUpdateArticle(ctx, input(platform.Object{"articleTitle": title, "articleContent": "正文 " + title, "status": status, "type": 1, "categoryName": category, "tagNameList": tags}))
	must(t, err)
	return count(t, db, "SELECT id FROM tb_article WHERE article_title=? ORDER BY id DESC LIMIT 1", title)
}
func TestArticleLifecycleAndAssociations(t *testing.T) {
	s, db, cfg := fixture(t)
	id := save(t, s, db, "草稿", 3, "Go", []string{"Go", "go", "中文"})
	if count(t, db, "SELECT count(*) FROM tb_article_tag WHERE article_id=?", id) != 2 {
		t.Fatal("draft must retain deduplicated tags")
	}
	v, err := s.GetArticleBackById(ctx, idInput(id))
	must(t, err)
	back := v.(platform.Object)
	if back["categoryName"] != "Go" || back["articleCover"] != cfg.PublicURL+"/files/defaults/cover.svg" || len(back["tagNameList"].([]string)) != 2 {
		t.Fatalf("edit projection=%v", back)
	}
	_, err = s.GetArticleById(ctx, idInput(id))
	code(t, err, platform.CodeFail)
	_, err = db.Exec("INSERT INTO tb_user_info(id,nickname,avatar,create_time) VALUES(7,'Editor','','2026-09-12T00:00:00')")
	must(t, err)
	edit := input(platform.Object{"id": id, "articleTitle": "公开文章", "articleContent": strings.Repeat("字", 501), "status": 1, "type": 2, "categoryName": "数据库", "tagNameList": []string{"SQLite"}, "isTop": 1, "originalUrl": "https://source.example/article"})
	edit.Actor.UserInfoID = 7
	_, err = s.SaveOrUpdateArticle(ctx, edit)
	must(t, err)
	if count(t, db, "SELECT user_id FROM tb_article WHERE id=?", id) != 7 || count(t, db, "SELECT count(*) FROM tb_article_tag WHERE article_id=?", id) != 1 {
		t.Fatal("author or associations not rebuilt")
	}
	v, err = s.ListArticles(ctx, platform.Input{})
	must(t, err)
	home := v.([]platform.Object)
	if len(home) != 1 || len([]rune(home[0]["articleContent"].(string))) != 500 || home[0]["categoryName"] != "数据库" {
		t.Fatalf("home=%v", home)
	}
	for i := 0; i < 2; i++ {
		v, err = s.GetArticleById(ctx, idInput(id))
		must(t, err)
		if v.(platform.Object)["viewsCount"] != int64(1) {
			t.Fatal("same browser counted twice")
		}
	}
	in := idInput(id)
	in.VisitorKey = "browser-b"
	v, err = s.GetArticleById(ctx, in)
	must(t, err)
	if v.(platform.Object)["viewsCount"] != int64(2) {
		t.Fatal("new visitor not counted")
	}
	for _, want := range []int64{1, 0} {
		_, err = s.SaveArticleLike(ctx, idInput(id))
		must(t, err)
		v, err = s.GetArticleById(ctx, idInput(id))
		must(t, err)
		if v.(platform.Object)["likeCount"] != want {
			t.Fatal("like toggle and detail differ")
		}
	}
	for _, deleted := range []int{1, 0} {
		_, err = s.UpdateArticleDelete(ctx, input(platform.Object{"idList": []int64{id}, "isDelete": deleted}))
		must(t, err)
		if count(t, db, "SELECT is_top FROM tb_article WHERE id=?", id) != 0 || count(t, db, "SELECT status FROM tb_article WHERE id=?", id) != 1 {
			t.Fatal("trash altered status or retained pin")
		}
		_, err = s.GetArticleById(ctx, idInput(id))
		if deleted == 1 {
			code(t, err, platform.CodeFail)
		} else {
			must(t, err)
		}
	}
	reopened, err := platform.OpenSQLite(cfg)
	must(t, err)
	defer reopened.Close()
	v, err = service(reopened, cfg).GetArticleBackById(ctx, idInput(id))
	must(t, err)
	if v.(platform.Object)["articleTitle"] != "公开文章" {
		t.Fatal("article lost after reopen")
	}
}
func TestArticlePublicFilteringPagingAndSearch(t *testing.T) {
	s, db, _ := fixture(t)
	ids := []int64{}
	for _, state := range []int{1, 2, 3, 1} {
		ids = append(ids, save(t, s, db, fmt.Sprint("字%_\\ <script>", state, len(ids)), state, "", nil))
	}
	_, err := s.UpdateArticleDelete(ctx, input(platform.Object{"idList": []int64{ids[3]}, "isDelete": 1}))
	must(t, err)
	older := save(t, s, db, "older", 1, "", nil)
	_, err = db.Exec("UPDATE tb_article SET create_time='2020-01-01T00:00:00',is_top=1 WHERE id=?", older)
	must(t, err)
	v, err := s.ListArticles(ctx, platform.Input{})
	must(t, err)
	rows := v.([]platform.Object)
	if len(rows) != 2 || rows[0]["id"] != older || rows[1]["categoryId"] != nil || len(rows[1]["tagDTOList"].([]platform.Object)) != 0 {
		t.Fatalf("public orphan projection=%v", rows)
	}
	v, err = s.ListArchives(ctx, platform.Input{})
	must(t, err)
	archives := v.(platform.Page)
	if archives.Count != 2 || archives.RecordList[0]["id"] != ids[0] {
		t.Fatalf("archives=%v", archives)
	}
	v, err = s.ListArticlesBySearch(ctx, platform.Input{Query: url.Values{"keywords": {"%_\\"}}})
	must(t, err)
	search := v.([]platform.Object)
	if len(search) != 1 || strings.Contains(search[0]["articleTitle"].(string), "<script>") || !strings.Contains(search[0]["articleTitle"].(string), "<span style='color:#f47466'>%_\\</span>") {
		t.Fatalf("unsafe or wildcard search=%v", search)
	}
	v, err = s.ListArticlesBySearch(ctx, platform.Input{Query: url.Values{"keywords": {"<SCRIPT>"}}})
	must(t, err)
	search = v.([]platform.Object)
	if len(search) != 1 || !strings.Contains(search[0]["articleTitle"].(string), "<span style='color:#f47466'>&lt;script&gt;</span>") {
		t.Fatal("highlight must match SQLite's ASCII case-insensitive search")
	}
	v, err = s.ListArticlesBySearch(ctx, platform.Input{})
	must(t, err)
	if v.([]platform.Object) == nil || len(v.([]platform.Object)) != 0 {
		t.Fatal("blank search must be []")
	}
	for _, id := range ids[1:] {
		_, err = s.GetArticleById(ctx, idInput(id))
		code(t, err, platform.CodeFail)
	}
	if count(t, db, "SELECT count(*) FROM tb_article_view") != 0 {
		t.Fatal("invisible detail recorded view")
	}
	v, err = s.GetArticleById(ctx, idInput(ids[0]))
	must(t, err)
	detail := v.(platform.Object)
	if detail["nextArticle"].(platform.Object)["id"] != older || len(detail["newestArticleList"].([]platform.Object)) != 2 {
		t.Fatal("private content leaked through neighbors/latest")
	}
	for _, endpoint := range []platform.Endpoint{s.ListArticles, s.ListArchives, s.ListArticleBacks} {
		v, err = endpoint(ctx, platform.Input{Query: url.Values{"current": {"9223372036854775807"}, "size": {"100"}}})
		must(t, err)
		switch p := v.(type) {
		case platform.Page:
			if len(p.RecordList) != 0 || p.Count == 0 {
				t.Fatal("far page lost true count")
			}
		case []platform.Object:
			if len(p) != 0 {
				t.Fatal("overflow page returned rows")
			}
		}
		for _, q := range []url.Values{{"current": {"0"}}, {"size": {"101"}}, {"size": {"abc"}}} {
			_, err = endpoint(ctx, platform.Input{Query: q})
			code(t, err, platform.CodeInvalid)
		}
	}
}
func TestArticleConditionsRecommendationsAndAdminFilters(t *testing.T) {
	s, db, _ := fixture(t)
	id := save(t, s, db, "one", 1, "Go", []string{"shared"})
	other := save(t, s, db, "two", 1, "Go", []string{"shared", "extra"})
	save(t, s, db, "private", 2, "Go", []string{"shared"})
	category := count(t, db, "SELECT id FROM tb_category WHERE category_name='Go'")
	tag := count(t, db, "SELECT id FROM tb_tag WHERE tag_name='shared'")
	for key, value := range map[string]int64{"categoryId": category, "tagId": tag} {
		v, err := s.ListArticlesByCondition(ctx, platform.Input{Query: url.Values{key: {fmt.Sprint(value)}}})
		must(t, err)
		p := v.(platform.Object)
		rows := p["articlePreviewDTOList"].([]platform.Object)
		if len(rows) != 2 || rows[0]["id"] != other || rows[0]["createTime"] != fixedTime.UnixMilli() {
			t.Fatalf("condition=%v", p)
		}
	}
	v, err := s.GetArticleById(ctx, idInput(id))
	must(t, err)
	if rows := v.(platform.Object)["recommendArticleList"].([]platform.Object); len(rows) != 1 || rows[0]["id"] != other {
		t.Fatalf("recommendations=%v", rows)
	}
	v, err = s.ListArticleBacks(ctx, platform.Input{Query: url.Values{"status": {"2"}, "categoryId": {fmt.Sprint(category)}, "tagId": {fmt.Sprint(tag)}, "type": {"1"}, "keywords": {"private"}}})
	must(t, err)
	if v.(platform.Page).Count != 1 {
		t.Fatal("admin filter mismatch")
	}
	v, err = s.ListArticlesByCondition(ctx, platform.Input{Query: url.Values{"categoryId": {"999999"}}})
	must(t, err)
	if v.(platform.Object)["name"] != nil || len(v.(platform.Object)["articlePreviewDTOList"].([]platform.Object)) != 0 {
		t.Fatal("missing category must have empty result")
	}
}
func TestArticleValidationRollbackAndDeletion(t *testing.T) {
	s, db, _ := fixture(t)
	id := save(t, s, db, "kept", 1, "Go", []string{"tag"})
	for _, body := range []any{nil, []int{}, platform.Object{"articleTitle": "", "articleContent": "body", "status": 1}, platform.Object{"articleTitle": strings.Repeat("字", 51), "articleContent": "body", "status": 1}, platform.Object{"articleTitle": "new", "articleContent": " ", "status": 1}, platform.Object{"articleTitle": "new", "articleContent": "body", "status": 4}, platform.Object{"id": 0, "articleTitle": "new", "articleContent": "body", "status": 1}} {
		_, err := s.SaveOrUpdateArticle(ctx, input(body))
		code(t, err, platform.CodeInvalid)
	}
	_, err := s.SaveOrUpdateArticle(ctx, platform.Input{Body: input(platform.Object{"articleTitle": "bad actor", "articleContent": "body", "status": 1}).Body})
	code(t, err, platform.CodeFail)
	_, err = s.SaveOrUpdateArticle(ctx, input(platform.Object{"id": 999999, "articleTitle": "missing", "articleContent": "body", "status": 1, "categoryName": "rollback"}))
	code(t, err, platform.CodeFail)
	_, err = s.SaveOrUpdateArticle(ctx, input(platform.Object{"id": id, "articleTitle": "partial", "articleContent": "body", "status": 1, "categoryName": "rollback", "tagNameList": []string{""}}))
	code(t, err, platform.CodeInvalid)
	if count(t, db, "SELECT count(*) FROM tb_category WHERE category_name='rollback'") != 0 || count(t, db, "SELECT count(*) FROM tb_article WHERE article_title='kept'") != 1 {
		t.Fatal("failed save leaked state")
	}
	_, err = s.UpdateArticleTop(ctx, input(platform.Object{"id": id, "isTop": 1}))
	must(t, err)
	_, err = s.UpdateArticleDelete(ctx, input(platform.Object{"idList": []int64{id, 999999}, "isDelete": 1}))
	code(t, err, platform.CodeFail)
	if count(t, db, "SELECT is_top FROM tb_article WHERE id=?", id) != 1 {
		t.Fatal("batch partial write")
	}
	_, err = db.Exec("INSERT INTO tb_comment(id,user_id,topic_id,comment_content,type,create_time) VALUES(101,1,?,'parent',1,'2026-09-12T00:00:00')", id)
	must(t, err)
	_, err = db.Exec("INSERT INTO tb_comment(id,user_id,topic_id,parent_id,comment_content,type,create_time) VALUES(102,1,?,101,'reply',1,'2026-09-12T00:00:00')", id)
	must(t, err)
	for _, target := range [][2]int64{{1, id}, {2, 101}, {2, 102}} {
		_, err = db.Exec("INSERT INTO tb_like(user_info_id,target_type,target_id,create_time) VALUES(1,?,?,'2026-09-12T00:00:00')", target[0], target[1])
		must(t, err)
	}
	_, err = s.GetArticleById(ctx, idInput(id))
	must(t, err)
	_, err = s.DeleteArticles(ctx, input([]int64{id, 999999}))
	code(t, err, platform.CodeFail)
	_, err = db.Exec("CREATE TRIGGER reject_article_delete BEFORE DELETE ON tb_article BEGIN SELECT RAISE(ABORT,'forced delete failure'); END")
	must(t, err)
	_, err = s.DeleteArticles(ctx, input([]int64{id}))
	if err == nil {
		t.Fatal("permanent delete failure ignored")
	}
	if count(t, db, "SELECT count(*) FROM tb_comment") != 2 || count(t, db, "SELECT count(*) FROM tb_like") != 3 || count(t, db, "SELECT count(*) FROM tb_article_view") != 1 {
		t.Fatal("failed deletion did not roll back dependent cleanup")
	}
	_, err = db.Exec("DROP TRIGGER reject_article_delete")
	must(t, err)
	_, err = s.DeleteArticles(ctx, input([]int64{id, id}))
	must(t, err)
	for _, table := range []string{"tb_article", "tb_article_tag", "tb_article_view", "tb_comment", "tb_like"} {
		if count(t, db, "SELECT count(*) FROM "+table) != 0 {
			t.Fatalf("orphan data in %s", table)
		}
	}
	if count(t, db, "SELECT count(*) FROM tb_category") != 1 || count(t, db, "SELECT count(*) FROM tb_tag") != 1 {
		t.Fatal("shared catalog records removed")
	}
	_, err = s.GetArticleBackById(ctx, idInput(id))
	code(t, err, platform.CodeFail)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.ListArticles(cancelled, platform.Input{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestArticleMutationBoundaryValidation(t *testing.T) {
	s, db, _ := fixture(t)
	id := save(t, s, db, strings.Repeat("字", 50), 2, "", nil)
	for _, body := range []any{nil, platform.Object{"id": id}, platform.Object{"id": id, "isTop": 2}, platform.Object{"id": 0, "isTop": 1}} {
		_, err := s.UpdateArticleTop(ctx, input(body))
		code(t, err, platform.CodeInvalid)
	}
	for _, body := range []any{nil, platform.Object{"idList": []int64{}, "isDelete": 1}, platform.Object{"idList": []int64{id}, "isDelete": 2}, platform.Object{"idList": []int64{id}}} {
		_, err := s.UpdateArticleDelete(ctx, input(body))
		code(t, err, platform.CodeInvalid)
	}
	for _, body := range []any{nil, []int64{}, []int64{0}, platform.Object{"id": id}} {
		_, err := s.DeleteArticles(ctx, input(body))
		code(t, err, platform.CodeInvalid)
	}
	_, err := s.SaveArticleLike(ctx, idInput(id))
	code(t, err, platform.CodeFail)
	in := idInput(id)
	in.Actor = platform.Actor{}
	_, err = s.SaveArticleLike(ctx, in)
	code(t, err, platform.CodeFail)
	for _, endpoint := range []platform.Endpoint{s.GetArticleById, s.GetArticleBackById} {
		_, err = endpoint(ctx, platform.Input{Params: map[string]string{"articleId": "bad"}})
		code(t, err, platform.CodeInvalid)
	}
	_, err = s.ListArticlesByCondition(ctx, platform.Input{})
	code(t, err, platform.CodeInvalid)
	_, err = s.UpdateArticleDelete(ctx, input(platform.Object{"idList": []int64{id}, "isDelete": 1}))
	must(t, err)
	_, err = s.UpdateArticleTop(ctx, input(platform.Object{"id": id, "isTop": 1}))
	code(t, err, platform.CodeFail)
	if count(t, db, "SELECT count(*) FROM tb_like") != 0 || count(t, db, "SELECT count(*) FROM tb_article_view") != 0 {
		t.Fatal("invalid interactions created facts")
	}
	_, err = site.New(db, platform.Config{}, nil).UpdateWebsiteConfig(ctx, input(platform.Object{"articleCover": "https://image.example/new.png"}))
	must(t, err)
	newID := save(t, s, db, "new config", 1, "", nil)
	v, err := s.GetArticleBackById(ctx, idInput(newID))
	must(t, err)
	if v.(platform.Object)["articleCover"] != "https://image.example/new.png" {
		t.Fatal("default cover was cached")
	}
}
func TestArticleConcurrentWritesAndRoutePolicies(t *testing.T) {
	s, db, cfg := fixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.SaveOrUpdateArticle(ctx, input(platform.Object{"articleTitle": fmt.Sprint(i), "articleContent": "body", "status": 3, "categoryName": "shared", "tagNameList": []string{"same"}}))
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if count(t, db, "SELECT count(*) FROM tb_article") != 8 || count(t, db, "SELECT count(*) FROM tb_category") != 1 || count(t, db, "SELECT count(*) FROM tb_tag") != 1 {
		t.Fatal("concurrent association deduplication failed")
	}
	routes := s.Routes()
	if len(routes) != len(article.RouteSpecs) {
		t.Fatal("missing routes")
	}
	for i, r := range routes {
		if !reflect.DeepEqual(r.Spec, article.RouteSpecs[i]) || r.Handle == nil {
			t.Fatal("route contract changed")
		}
	}
	router, err := platform.NewRouter(routes, platform.Callbacks{}, cfg)
	must(t, err)
	for _, path := range []string{"/admin/articles", "/articles/1/like"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		var out platform.Result
		must(t, json.Unmarshal(w.Body.Bytes(), &out))
		if out.Flag || out.Code != platform.CodeFail {
			t.Fatalf("unauthorized route=%s %s", path, w.Body.String())
		}
	}
}
