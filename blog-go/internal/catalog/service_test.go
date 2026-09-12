package catalog_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"blog-go/internal/article"
	"blog-go/internal/catalog"
	"blog-go/internal/identity"
	"blog-go/internal/platform"
)

func setup(t *testing.T) (*catalog.Service, *sql.DB, platform.Config) {
	t.Helper()
	cfg := platform.Config{DataDir: t.TempDir(), TimeZone: time.FixedZone("Asia/Shanghai", 8*60*60)}
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := platform.Migrate(context.Background(), db, []string{identity.Schema, catalog.Schema, article.Schema}, nil); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 9, 10, 5, 6, 7, 0, time.UTC) }
	return catalog.New(db, cfg, now), db, cfg
}

func input(value any) platform.Input {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return platform.Input{Body: body, Query: make(url.Values)}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func code(t *testing.T, err error, want int) {
	t.Helper()
	var business *platform.Error
	if !errors.As(err, &business) || business.Code != want {
		t.Fatalf("error=%v, want code %d", err, want)
	}
}

func queryCount(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var count int64
	must(t, db.QueryRow(query, args...).Scan(&count))
	return count
}

func TestNamesCRUDValidationAndOriginalProjections(t *testing.T) {
	service, db, _ := setup(t)
	ctx := context.Background()
	for _, test := range []struct {
		name, table, field, column string
		save, list, search         platform.Endpoint
	}{
		{"category", "tb_category", "categoryName", "category_name", service.SaveOrUpdateCategory, service.ListBackCategories, service.ListCategoriesBySearch},
		{"tag", "tb_tag", "tagName", "tag_name", service.SaveOrUpdateTag, service.ListTagBackDTO, service.ListTagsBySearch},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.save(ctx, input(platform.Object{test.field: " Go "}))
			must(t, err)
			_, err = test.save(ctx, input(platform.Object{test.field: "go"}))
			must(t, err)
			if count := queryCount(t, db, "SELECT count(*) FROM "+test.table); count != 1 {
				t.Fatalf("case duplicate inserted: %d", count)
			}
			id := queryCount(t, db, "SELECT id FROM "+test.table+" WHERE "+test.column+"='Go'")
			_, err = test.save(ctx, input(platform.Object{"id": id, test.field: " Go Lang "}))
			must(t, err)
			if queryCount(t, db, "SELECT count(*) FROM "+test.table+" WHERE id=? AND "+test.column+"='Go Lang'", id) != 1 {
				t.Fatal("rename not persisted")
			}
			_, err = test.save(ctx, input(platform.Object{test.field: "Other"}))
			must(t, err)
			_, err = test.save(ctx, input(platform.Object{"id": id, test.field: "OTHER"}))
			code(t, err, platform.CodeFail)
			if queryCount(t, db, "SELECT count(*) FROM "+test.table+" WHERE id=? AND "+test.column+"='Go Lang'", id) != 1 {
				t.Fatal("rejected rename changed an existing identity")
			}
			_, err = test.save(ctx, input(platform.Object{"id": 999999, test.field: "Missing"}))
			code(t, err, platform.CodeFail)
			for _, value := range []any{nil, "", " \n ", strings.Repeat("字", 21), 12} {
				_, err = test.save(ctx, input(platform.Object{test.field: value}))
				code(t, err, platform.CodeInvalid)
			}
			_, err = test.save(ctx, input(platform.Object{test.field: strings.Repeat("字", 20)}))
			must(t, err)
			for _, value := range []any{0, -1, "1", 1.25} {
				_, err = test.save(ctx, input(platform.Object{"id": value, test.field: "Valid"}))
				code(t, err, platform.CodeInvalid)
			}
			_, err = test.save(ctx, input(platform.Object{test.field: "50%_\\"}))
			must(t, err)
			result, err := test.search(ctx, platform.Input{Query: url.Values{"keywords": {"%_\\"}}})
			must(t, err)
			options := result.([]platform.Object)
			if len(options) != 1 || len(options[0]) != 2 || options[0][test.field] != "50%_\\" {
				t.Fatalf("candidate fields/search changed: %v", options)
			}
			result, err = test.list(ctx, platform.Input{Query: url.Values{"current": {"1"}, "size": {"1"}, "keywords": {"Go"}}})
			must(t, err)
			page := result.(platform.Page)
			if page.Count != 1 || len(page.RecordList) != 1 || page.RecordList[0]["createTime"] != "2026-09-10T13:06:07" {
				t.Fatalf("admin projection: %+v", page)
			}
			result, err = test.list(ctx, platform.Input{Query: url.Values{"current": {"9223372036854775807"}, "size": {"100"}, "keywords": {"Go"}}})
			must(t, err)
			page = result.(platform.Page)
			if page.Count != 1 || page.RecordList == nil || len(page.RecordList) != 0 {
				t.Fatalf("far page lost true count: %+v", page)
			}
		})
	}
}

func TestPublicCountsAndBatchDeletionProtectAllArticleAssociations(t *testing.T) {
	service, db, _ := setup(t)
	ctx := context.Background()
	for _, name := range []string{"Used", "Free"} {
		_, err := service.SaveOrUpdateCategory(ctx, input(platform.Object{"categoryName": name}))
		must(t, err)
		_, err = service.SaveOrUpdateTag(ctx, input(platform.Object{"tagName": name}))
		must(t, err)
	}
	_, err := db.Exec("INSERT INTO tb_user_info(id,nickname,avatar,create_time) VALUES(71,'Author','','2026-09-10T00:00:00')")
	must(t, err)
	for index, state := range [][2]int{{1, 0}, {2, 0}, {3, 0}, {1, 1}} {
		_, err := db.Exec("INSERT INTO tb_article(id,user_id,category_id,article_title,article_content,status,is_delete,create_time) VALUES(?,71,1,'Article','Body',?,?,'2026-09-10T00:00:00')", index+1, state[0], state[1])
		must(t, err)
		_, err = db.Exec("INSERT INTO tb_article_tag(article_id,tag_id) VALUES(?,1)", index+1)
		must(t, err)
	}
	got, err := service.ListCategories(ctx, platform.Input{})
	must(t, err)
	public := got.(platform.Page)
	if public.Count != 2 || len(public.RecordList) != 2 || public.RecordList[0]["articleCount"] != int64(1) || public.RecordList[1]["articleCount"] != int64(0) {
		t.Fatalf("public visibility count: %+v", public)
	}
	got, err = service.ListBackCategories(ctx, platform.Input{Query: url.Values{"keywords": {"Used"}}})
	must(t, err)
	if got.(platform.Page).RecordList[0]["articleCount"] != int64(4) {
		t.Fatal("admin category count excludes existing associations")
	}
	got, err = service.ListTags(ctx, platform.Input{})
	must(t, err)
	tags := got.(platform.Page)
	if tags.Count != 2 || len(tags.RecordList[0]) != 2 {
		t.Fatalf("public TagDTO changed: %+v", tags)
	}
	got, err = service.ListTagBackDTO(ctx, platform.Input{Query: url.Values{"keywords": {"Used"}}})
	must(t, err)
	if got.(platform.Page).RecordList[0]["articleCount"] != int64(4) {
		t.Fatal("admin tag count incorrect")
	}
	_, err = service.DeleteCategories(ctx, input([]int64{2, 1}))
	code(t, err, platform.CodeFail)
	_, err = service.DeleteTag(ctx, input([]int64{2, 1}))
	code(t, err, platform.CodeFail)
	if queryCount(t, db, "SELECT count(*) FROM tb_category") != 2 || queryCount(t, db, "SELECT count(*) FROM tb_tag") != 2 {
		t.Fatal("failed batch partially deleted names")
	}
	_, err = service.DeleteCategories(ctx, input([]int64{2, 9999}))
	code(t, err, platform.CodeFail)
	if queryCount(t, db, "SELECT count(*) FROM tb_category WHERE id=2") != 1 {
		t.Fatal("missing target caused partial deletion")
	}
	_, err = db.Exec("DELETE FROM tb_article")
	must(t, err)
	_, err = service.DeleteCategories(ctx, input([]int64{2, 1, 2}))
	must(t, err)
	_, err = service.DeleteTag(ctx, input([]int64{2, 1}))
	must(t, err)
	for _, endpoint := range []platform.Endpoint{service.ListCategories, service.ListTags, service.ListBackCategories, service.ListTagBackDTO} {
		got, err := endpoint(ctx, platform.Input{})
		must(t, err)
		page := got.(platform.Page)
		if page.Count != 0 || page.RecordList == nil || len(page.RecordList) != 0 {
			t.Fatal("empty page shape changed")
		}
	}
	for _, endpoint := range []platform.Endpoint{service.DeleteCategories, service.DeleteTag, service.DeleteFriendLink} {
		for _, body := range []any{[]int64{}, []int64{0}, []int64{-1}, platform.Object{"idList": []int64{1}}, nil, []string{"1"}} {
			_, err := endpoint(ctx, input(body))
			code(t, err, platform.CodeInvalid)
		}
	}
}

func TestResolveNamesUsesCallerTransactionAndDeduplicatesBySQLiteIdentity(t *testing.T) {
	service, db, _ := setup(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	must(t, err)
	category, tags, err := service.ResolveNamesTx(ctx, tx, " Go ", []string{" Go ", "go", "中文", "中文", "Ä", "ä"})
	must(t, err)
	if category == nil || len(tags) != 4 {
		t.Fatalf("wrong identities: %v %v", category, tags)
	}
	again, sameTags, err := service.ResolveNamesTx(ctx, tx, "go", []string{"GO"})
	must(t, err)
	if again == nil || *again != *category || len(sameTags) != 1 || sameTags[0] != tags[0] {
		t.Fatal("case variants did not reuse existing names")
	}
	must(t, tx.Rollback())
	if queryCount(t, db, "SELECT count(*) FROM tb_category") != 0 || queryCount(t, db, "SELECT count(*) FROM tb_tag") != 0 {
		t.Fatal("resolver committed the caller transaction")
	}
	tx, err = db.BeginTx(ctx, nil)
	must(t, err)
	_, _, err = service.ResolveNamesTx(ctx, tx, "New category", []string{"Valid", strings.Repeat("字", 21)})
	code(t, err, platform.CodeInvalid)
	var count int
	must(t, tx.QueryRow("SELECT count(*) FROM tb_category").Scan(&count))
	if count != 0 {
		t.Fatal("late validation left a partial category")
	}
	category, tags, err = service.ResolveNamesTx(ctx, tx, "", nil)
	must(t, err)
	if category != nil || tags == nil || len(tags) != 0 {
		t.Fatal("optional article metadata changed")
	}
	must(t, tx.Commit())
}

func TestFriendLinksPersistAndValidateOriginalFields(t *testing.T) {
	service, db, cfg := setup(t)
	ctx := context.Background()
	link := platform.Object{"linkName": " Friend ", "linkAvatar": "https://example.com/avatar.png", "linkAddress": "https://example.com", "linkIntro": " Introduction "}
	_, err := service.SaveOrUpdateFriendLink(ctx, input(link))
	must(t, err)
	got, err := service.ListFriendLinks(ctx, platform.Input{})
	must(t, err)
	list := got.([]platform.Object)
	if len(list) != 1 || len(list[0]) != 5 || list[0]["linkName"] != "Friend" || list[0]["linkIntro"] != "Introduction" {
		t.Fatalf("public links: %v", list)
	}
	link["id"], link["linkName"] = list[0]["id"], "Updated"
	_, err = service.SaveOrUpdateFriendLink(ctx, input(link))
	must(t, err)
	got, err = service.ListFriendLinkDTO(ctx, platform.Input{Query: url.Values{"keywords": {"Updated"}}})
	must(t, err)
	page := got.(platform.Page)
	if page.Count != 1 || len(page.RecordList[0]) != 6 || page.RecordList[0]["createTime"] != "2026-09-10T13:06:07" {
		t.Fatalf("admin links: %+v", page)
	}
	for _, change := range []platform.Object{
		{"linkName": ""}, {"linkIntro": ""}, {"linkAvatar": "javascript:alert(1)"}, {"linkAddress": "/relative"},
		{"linkAddress": "https://user:secret@example.com"}, {"linkName": strings.Repeat("字", 21)},
		{"linkAddress": "https://example.com/" + strings.Repeat("x", 50)}, {"linkIntro": strings.Repeat("字", 101)},
	} {
		candidate := make(platform.Object)
		for key, value := range link {
			candidate[key] = value
		}
		for key, value := range change {
			candidate[key] = value
		}
		_, err = service.SaveOrUpdateFriendLink(ctx, input(candidate))
		code(t, err, platform.CodeInvalid)
	}
	must(t, db.Close())
	reopened, err := platform.OpenSQLite(cfg)
	must(t, err)
	defer reopened.Close()
	restarted := catalog.New(reopened, cfg, nil)
	got, err = restarted.ListFriendLinks(ctx, platform.Input{})
	must(t, err)
	if got.([]platform.Object)[0]["linkName"] != "Updated" {
		t.Fatal("restart lost link edit")
	}
	_, err = restarted.DeleteFriendLink(ctx, input([]int64{list[0]["id"].(int64)}))
	must(t, err)
	got, err = restarted.ListFriendLinks(ctx, platform.Input{})
	must(t, err)
	if got.([]platform.Object) == nil || len(got.([]platform.Object)) != 0 {
		t.Fatal("empty links must be an array")
	}
}

func TestConcurrentNameCreationAndCancellation(t *testing.T) {
	service, db, _ := setup(t)
	var workers sync.WaitGroup
	failures := make(chan error, 16)
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := service.SaveOrUpdateCategory(context.Background(), input(platform.Object{"categoryName": " Concurrent "}))
			failures <- err
		}()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		must(t, err)
	}
	if queryCount(t, db, "SELECT count(*) FROM tb_category") != 1 {
		t.Fatal("concurrent duplicate names")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.SaveOrUpdateTag(ctx, input(platform.Object{"tagName": "Cancelled"}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if queryCount(t, db, "SELECT count(*) FROM tb_tag") != 0 {
		t.Fatal("cancelled write committed")
	}
	must(t, db.Close())
	if _, err := service.ListCategories(context.Background(), platform.Input{}); err == nil {
		t.Fatal("closed database produced fake success")
	}
}

func TestAllCatalogRoutesUseRealBehaviorAndKeepAuthorization(t *testing.T) {
	service, _, cfg := setup(t)
	allowed := true
	engine, err := platform.NewRouter(service.Routes(), platform.Callbacks{
		Resolve: func(*http.Request) (platform.Actor, error) { return platform.Actor{LoggedIn: true, UserInfoID: 1}, nil },
		Authorize: func(context.Context, platform.Actor, string, string) error {
			if !allowed {
				return &platform.Error{Code: platform.CodeFail, Message: "Denied"}
			}
			return nil
		},
	}, cfg)
	must(t, err)
	do := func(method, path string, body any, expectedCode int) any {
		t.Helper()
		payload := ""
		if body != nil {
			raw, err := json.Marshal(body)
			must(t, err)
			payload = string(raw)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(payload))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)
		var result platform.Result
		must(t, json.Unmarshal(recorder.Body.Bytes(), &result))
		if recorder.Code != 200 || result.Code != expectedCode || result.Flag != (expectedCode == platform.CodeOK) {
			t.Fatalf("%s %s: HTTP %d %+v", method, path, recorder.Code, result)
		}
		return result.Data
	}
	do("POST", "/admin/categories", platform.Object{"categoryName": "Cat"}, platform.CodeOK)
	do("POST", "/admin/tags", platform.Object{"tagName": "Tag"}, platform.CodeOK)
	do("POST", "/admin/links", platform.Object{"linkName": "Link", "linkAvatar": "https://example.com/a.png", "linkAddress": "https://example.com", "linkIntro": "Intro"}, platform.CodeOK)
	for _, path := range []string{"/categories", "/tags", "/admin/categories", "/admin/tags", "/admin/links"} {
		data := do("GET", path, nil, platform.CodeOK).(map[string]any)
		if data["count"] != float64(1) || len(data["recordList"].([]any)) != 1 {
			t.Fatalf("%s lost persisted rows", path)
		}
	}
	for _, path := range []string{"/links", "/admin/categories/search", "/admin/tags/search"} {
		if len(do("GET", path, nil, platform.CodeOK).([]any)) != 1 {
			t.Fatalf("%s lost list data", path)
		}
	}
	allowed = false
	for _, path := range []string{"/admin/categories", "/admin/tags", "/admin/links"} {
		do("POST", path, platform.Object{}, platform.CodeFail)
		do("DELETE", path, []int64{1}, platform.CodeFail)
	}
	allowed = true
	for _, path := range []string{"/admin/categories", "/admin/tags", "/admin/links"} {
		do("DELETE", path, []int64{1}, platform.CodeOK)
	}
	for _, route := range service.Routes() {
		if route.Handle == nil {
			t.Fatalf("unbound %s", fmt.Sprint(route.Spec))
		}
	}
}

func TestLargeDeletionValidatesEveryBatchBeforeCommit(t *testing.T) {
	service, db, _ := setup(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	must(t, err)
	statement, err := tx.PrepareContext(ctx, "INSERT INTO tb_category(id,category_name,create_time) VALUES(?,?,'2026-09-10T00:00:00')")
	must(t, err)
	ids := make([]int64, 600)
	for index := range ids {
		ids[index] = int64(index + 1)
		_, err = statement.ExecContext(ctx, ids[index], fmt.Sprintf("Category %d", index+1))
		must(t, err)
	}
	must(t, statement.Close())
	_, err = tx.ExecContext(ctx, "INSERT INTO tb_user_info(id,nickname,avatar,create_time) VALUES(71,'Author','','2026-09-10T00:00:00')")
	must(t, err)
	_, err = tx.ExecContext(ctx, "INSERT INTO tb_article(user_id,category_id,article_title,article_content,status,is_delete,create_time) VALUES(71,600,'Hidden','Body',3,1,'2026-09-10T00:00:00')")
	must(t, err)
	must(t, tx.Commit())
	_, err = service.DeleteCategories(ctx, input(ids))
	code(t, err, platform.CodeFail)
	if queryCount(t, db, "SELECT count(*) FROM tb_category") != 600 {
		t.Fatal("a late association check left a partially deleted batch")
	}
	_, err = db.Exec("DELETE FROM tb_article")
	must(t, err)
	_, err = service.DeleteCategories(ctx, input(ids))
	must(t, err)
	if queryCount(t, db, "SELECT count(*) FROM tb_category") != 0 {
		t.Fatal("large batch was not fully deleted")
	}
}

func TestInvalidPaginationAndMissingFriendLinkFailWithoutWrites(t *testing.T) {
	service, db, _ := setup(t)
	for _, endpoint := range []platform.Endpoint{service.ListBackCategories, service.ListTagBackDTO, service.ListFriendLinkDTO} {
		for _, query := range []url.Values{{"current": {"0"}}, {"current": {"1", "2"}}, {"size": {"101"}}, {"size": {"bad"}}} {
			_, err := endpoint(context.Background(), platform.Input{Query: query})
			code(t, err, platform.CodeInvalid)
		}
	}
	_, err := service.SaveOrUpdateFriendLink(context.Background(), input(platform.Object{
		"id": 999, "linkName": "Missing", "linkAvatar": "https://example.com/a.png",
		"linkAddress": "https://example.com", "linkIntro": "Missing target",
	}))
	code(t, err, platform.CodeFail)
	if queryCount(t, db, "SELECT count(*) FROM tb_friend_link") != 0 {
		t.Fatal("missing update target created a link")
	}
}
