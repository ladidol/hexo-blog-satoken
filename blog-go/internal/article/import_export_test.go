package article_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blog-go/internal/files"
	"blog-go/internal/platform"
)

func uploads(t *testing.T, typ string, names, bodies []string) platform.Input {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for i, name := range names {
		part, err := writer.CreateFormFile("file", name)
		must(t, err)
		_, err = part.Write([]byte(bodies[i]))
		must(t, err)
	}
	must(t, writer.Close())
	request := httptest.NewRequest("POST", "/admin/articles/import", &buf)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	must(t, request.ParseMultipartForm(1<<20))
	t.Cleanup(func() { request.MultipartForm.RemoveAll() })
	in := input(nil)
	in.Files = request.MultipartForm.File["file"]
	in.Form = url.Values{"type": {typ}}
	return in
}
func TestArticleImportNormalAndHexo(t *testing.T) {
	s, db, _ := fixture(t)
	_, err := s.ImportArticles(ctx, uploads(t, "", []string{"中文.笔记.markdown", "second.txt"}, []string{"# 你好\r\n精确正文\n", "normal body"}))
	must(t, err)
	var title, body string
	must(t, db.QueryRow("SELECT article_title,article_content FROM tb_article ORDER BY id LIMIT 1").Scan(&title, &body))
	if title != "中文.笔记" || body != "# 你好\r\n精确正文\n" || count(t, db, "SELECT count(*) FROM tb_article WHERE status=3") != 2 {
		t.Fatal("normal import changed title/body/state")
	}
	for _, yaml := range []string{"title: Hexo\ndate: 2020-01-02 03:04:05\ncategories: 技术\ntags: [Go, go, 文章]\nignored: {other: value}\n", "title: \"Hexo: 数组\"\ndate: 2020-01-02T03:04:05+08:00\ncategories: [软件, 后端]\ntags: Go\n"} {
		_, err = s.ImportArticles(ctx, uploads(t, "hexo", []string{"post.md"}, []string{"---\n" + yaml + "---\n# Hexo body\n"}))
		must(t, err)
	}
	if count(t, db, "SELECT count(*) FROM tb_article WHERE status=1 AND create_time='2020-01-02T03:04:05'") != 2 {
		t.Fatal("Hexo public status/date failed")
	}
	_, err = s.ImportArticles(ctx, uploads(t, "hexo", []string{"incomplete.md"}, []string{"---\ntitle: Incomplete\ncategories: Go\n---\nbody"}))
	must(t, err)
	id := count(t, db, "SELECT id FROM tb_article WHERE article_title='Incomplete'")
	v, err := s.GetArticleBackById(ctx, idInput(id))
	must(t, err)
	if v.(platform.Object)["status"] != 3 || v.(platform.Object)["categoryName"] != "Go" {
		t.Fatal("incomplete Hexo must remain editable draft")
	}
}
func TestArticleImportBatchRollbackAndValidation(t *testing.T) {
	for _, tc := range []struct{ name, typ, file, body string }{
		{"unsupported", "wordpress", "post.md", "body"}, {"extension", "", "file.png", "body"}, {"invalid utf8", "", "post.md", string([]byte{0xff})}, {"empty", "", "post.md", " "}, {"invalid yaml", "hexo", "post.md", "---\ntitle: [bad\n---\nbody"}, {"invalid date", "hexo", "post.md", "---\ntitle: bad\ndate: yesterday\n---\nbody"}, {"missing delimiter", "hexo", "post.md", "---\ntitle: bad\nbody"}, {"invalid tags", "hexo", "post.md", "---\ntitle: bad\ntags: {nested: invalid}\n---\nbody"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, _ := fixture(t)
			valid := "good body"
			if tc.typ == "hexo" {
				valid = "---\ntitle: good\ncategories: Created\ntags: Valid\n---\ngood body"
			}
			_, err := s.ImportArticles(ctx, uploads(t, tc.typ, []string{"valid.md", tc.file}, []string{valid, tc.body}))
			if err == nil {
				t.Fatal("invalid batch succeeded")
			}
			if count(t, db, "SELECT count(*) FROM tb_article") != 0 || count(t, db, "SELECT count(*) FROM tb_category") != 0 {
				t.Fatal("partial import survived")
			}
		})
	}
	s, db, _ := fixture(t)
	_, err := s.ImportArticles(ctx, input(nil))
	code(t, err, platform.CodeInvalid)
	_, err = db.Exec("CREATE TRIGGER reject_second BEFORE INSERT ON tb_article WHEN NEW.article_title='second' BEGIN SELECT RAISE(ABORT,'forced second-row failure'); END")
	must(t, err)
	_, err = s.ImportArticles(ctx, uploads(t, "", []string{"first.md", "second.md"}, []string{"body", "body"}))
	if err == nil {
		t.Fatal("database failure ignored")
	}
	if count(t, db, "SELECT count(*) FROM tb_article") != 0 {
		t.Fatal("partial transaction committed")
	}
}
func TestArticleExportDistinctKeysDownloadAndRestart(t *testing.T) {
	s, db, cfg := fixture(t)
	ids := []int64{}
	for i := 0; i < 2; i++ {
		ids = append(ids, save(t, s, db, "重复 标题", 3, "", nil))
	}
	const body = "# 精确 UTF-8\r\n正文\n"
	_, err := db.Exec("UPDATE tb_article SET article_content=?", body)
	must(t, err)
	value, err := s.ExportArticles(ctx, input(ids))
	must(t, err)
	urls := value.([]string)
	if len(urls) != 2 || urls[0] == urls[1] {
		t.Fatalf("distinct article exports=%v", urls)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	for i, address := range urls {
		u, err := url.Parse(address)
		must(t, err)
		if !strings.Contains(u.Path, fmt.Sprint(ids[i])+"-"+hash) {
			t.Fatalf("key missing article ID/content hash: %s", u.Path)
		}
		data, err := os.ReadFile(filepath.Join(cfg.DataDir, strings.TrimPrefix(u.Path, "/")))
		must(t, err)
		if string(data) != body {
			t.Fatal("export body changed")
		}
	}
	again, err := s.ExportArticles(ctx, input(ids))
	must(t, err)
	if strings.Join(again.([]string), ",") != strings.Join(urls, ",") {
		t.Fatal("repeat export changed keys")
	}
	storage := files.New(db, cfg, nil, nil)
	allow := false
	routes := []platform.Route{{Spec: platform.RouteSpec{Method: "GET", Path: "/files/{path...}", Operation: "ServeFile", Policy: "public"}, Raw: http.HandlerFunc(storage.ServeFile)}}
	router, err := platform.NewRouter(routes, platform.Callbacks{Resolve: func(r *http.Request) (platform.Actor, error) {
		cookie, e := r.Cookie(platform.TokenCookie)
		return platform.Actor{LoggedIn: e == nil && cookie.Value == "session", UserInfoID: 1}, nil
	}, Authorize: func(_ context.Context, _ platform.Actor, method, path string) error {
		if allow && method == "POST" && path == "/admin/articles/export" {
			return nil
		}
		return &platform.Error{Code: platform.CodeFail, Message: "denied"}
	}}, cfg)
	must(t, err)
	for _, logged := range []bool{false, true} {
		request := httptest.NewRequest("GET", urls[0], nil)
		if logged {
			request.AddCookie(&http.Cookie{Name: platform.TokenCookie, Value: "session"})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		if strings.Contains(w.Body.String(), "精确 UTF-8") {
			t.Fatal("private export leaked")
		}
	}
	allow = true
	request := httptest.NewRequest("GET", urls[0], nil)
	request.AddCookie(&http.Cookie{Name: platform.TokenCookie, Value: "session"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	_, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
	must(t, err)
	if w.Code != 200 || w.Body.String() != body || !strings.Contains(params["filename"], "重复 标题") {
		t.Fatalf("download=%d %v", w.Code, w.Header())
	}
	_, err = s.ExportArticles(ctx, input([]int64{ids[0], 999999}))
	code(t, err, platform.CodeFail)
	entries, err := os.ReadDir(filepath.Join(cfg.DataDir, "files", "exports"))
	must(t, err)
	if len(entries) != 2 {
		t.Fatal("missing target wrote files")
	}
}
func TestArticleExportCompensatesPartialStorageFailure(t *testing.T) {
	s, db, cfg := fixture(t)
	first := save(t, s, db, "first", 3, "", nil)
	second := save(t, s, db, "second", 3, "", nil)
	must(t, os.MkdirAll(filepath.Join(cfg.DataDir, "files", "exports"), 0700))
	body := "正文 second"
	key := fmt.Sprintf("%d-%x.md", second, sha256.Sum256([]byte(body)))
	must(t, os.Mkdir(filepath.Join(cfg.DataDir, "files", "exports", key), 0700))
	_, err := s.ExportArticles(ctx, input([]int64{first, second}))
	if err == nil {
		t.Fatal("storage failure ignored")
	}
	entries, err := os.ReadDir(filepath.Join(cfg.DataDir, "files", "exports"))
	must(t, err)
	if len(entries) != 1 || entries[0].Name() != key {
		t.Fatal("partial export not compensated")
	}
	_, err = s.ExportArticles(ctx, input([]int64{first}))
	must(t, err)
	_, err = s.ExportArticles(ctx, input([]int64{first, second}))
	if err == nil {
		t.Fatal("storage failure ignored")
	}
	entries, err = os.ReadDir(filepath.Join(cfg.DataDir, "files", "exports"))
	must(t, err)
	if len(entries) != 2 {
		t.Fatal("compensation removed a previously shared export")
	}
}
