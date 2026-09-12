package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"blog-go/internal/article"
	"blog-go/internal/catalog"
	"blog-go/internal/chat"
	"blog-go/internal/community"
	"blog-go/internal/files"
	"blog-go/internal/identity"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
	_ "github.com/mattn/go-sqlite3"
)

func pngData(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestArticleExportKeysIncludeArticleIdentity(t *testing.T) {
	cfg := config(t)
	s := files.New(nil, cfg, nil, nil)
	first, err := s.Put(context.Background(), "exports", "article-41-标题.md", "text/markdown", strings.NewReader("same body"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Put(context.Background(), "exports", "article-42-标题.md", "text/markdown", strings.NewReader("same body"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Key == second.Key || !strings.HasPrefix(first.Key, "exports/41-") || !strings.HasPrefix(second.Key, "exports/42-") {
		t.Fatalf("article identities missing from export keys: %v %v", first, second)
	}
	again, err := s.Put(context.Background(), "exports", "article-41-标题.md", "text/markdown", strings.NewReader("same body"))
	if err != nil {
		t.Fatal(err)
	}
	if again.Key != first.Key || again.Created {
		t.Fatal("repeat export is not stable")
	}
}
func config(t *testing.T) platform.Config {
	return platform.Config{DataDir: t.TempDir(), PublicURL: "https://blog.example", Values: map[string]string{"BLOG_UPLOAD_MODE": "local"}}
}
func fileDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, schema := range []string{identity.Schema, article.Schema, media.Schema, site.Schema, chat.Schema, catalog.Schema, community.Schema} {
		if _, err := db.Exec(schema); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func TestLocalFilesPersistDeduplicateAndRead(t *testing.T) {
	cfg := config(t)
	s := files.New(nil, cfg, nil, nil)
	data := pngData(t)
	f, err := s.Put(context.Background(), "articles", "图片.PNG", "image/png", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	wantKey := fmt.Sprintf("articles/%x.png", sha256.Sum256(data))
	if !f.Created || f.Key != wantKey || f.URL != cfg.PublicURL+"/files/"+wantKey {
		t.Fatalf("unexpected object: %+v", f)
	}
	again, err := s.Put(context.Background(), "articles", "renamed.png", "image/png", bytes.NewReader(data))
	if err != nil || again.Created || again.Key != f.Key {
		t.Fatalf("dedup: %+v %v", again, err)
	}
	restarted := files.New(nil, cfg, nil, nil)
	for _, method := range []string{"GET", "HEAD"} {
		w := httptest.NewRecorder()
		restarted.ServeFile(w, httptest.NewRequest(method, f.URL, nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Content-Length") != fmt.Sprint(len(data)) || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s headers: %d %v", method, w.Code, w.Header())
		}
		if method == "GET" && !bytes.Equal(w.Body.Bytes(), data) || method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("body mismatch")
		}
	}
	// recorderx's little-endian mono PCM WAV remains playable and supports ranges.
	wav := make([]byte, 48)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], 40)
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 8000)
	binary.LittleEndian.PutUint32(wav[28:], 16000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], 4)
	audio, err := s.Put(context.Background(), "voice", "record.wav", "audio/wav", bytes.NewReader(wav))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", audio.URL, nil)
	req.Header.Set("Range", "bytes=8-11")
	w := httptest.NewRecorder()
	s.ServeFile(w, req)
	if w.Code != 206 || w.Body.String() != "WAVE" || w.Header().Get("Content-Range") != "bytes 8-11/48" {
		t.Fatalf("range: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
}

type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type failReader struct{ err error }

func (r failReader) Read([]byte) (int, error) { return 0, r.err }
func TestFilesRejectInvalidInputWithoutObjects(t *testing.T) {
	cfg := config(t)
	s := files.New(nil, cfg, nil, nil)
	data := pngData(t)
	cases := []struct {
		scope, name, typ string
		data             []byte
	}{
		{"articles", "../bad.png", "image/png", data}, {"articles", "/bad.png", "image/png", data}, {"articles", "C:\\bad.png", "image/png", data}, {"articles", "bad\x00.png", "image/png", data},
		{"../db", "ok.png", "image/png", data}, {"defaults", "bad.png", "image/png", data}, {"articles", "bad.svg", "image/svg+xml", []byte("<svg/>")},
		{"articles", "bad.png", "image/png", []byte("<html>not a picture</html>")}, {"articles", "bad.png", "text/html", data},
		{"articles", "truncated.png", "image/png", data[:len(data)/2]}, {"articles", "empty.png", "image/png", nil}, {"voice", "image.png", "image/png", data},
	}
	for _, c := range cases {
		t.Run(c.scope+c.name+c.typ, func(t *testing.T) {
			if f, err := s.Put(context.Background(), c.scope, c.name, c.typ, bytes.NewReader(c.data)); err == nil || f.URL != "" {
				t.Fatalf("accepted invalid: %+v %v", f, err)
			}
		})
	}
	if _, err := s.Put(context.Background(), "voice", "huge.wav", "audio/wav", endlessReader{}); err == nil {
		t.Fatal("unbounded stream accepted")
	}
	boom := errors.New("read failure")
	if _, err := s.Put(context.Background(), "articles", "a.png", "image/png", failReader{boom}); !errors.Is(err, boom) {
		t.Fatalf("lost reader failure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Put(ctx, "articles", "a.png", "image/png", bytes.NewReader(data)); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	var count int
	_ = filepath.WalkDir(filepath.Join(cfg.DataDir, "files"), func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			count++
		}
		return nil
	})
	if count != 0 {
		t.Fatalf("invalid input left %d files", count)
	}
}
func TestLocalStoreConcurrentAtomicWritesAndCompensation(t *testing.T) {
	cfg := config(t)
	store := &files.LocalStore{Root: filepath.Join(cfg.DataDir, "files"), PublicURL: cfg.PublicURL}
	data := pngData(t)
	var wg sync.WaitGroup
	results := make(chan files.StoredFile, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := store.Put(context.Background(), "avatar", "a.png", "image/png", bytes.NewReader(data))
			results <- f
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var created files.StoredFile
	var n int
	for f := range results {
		if f.Created {
			created = f
			n++
		}
	}
	if n != 1 {
		t.Fatalf("Created count %d", n)
	}
	reuse := created
	reuse.Created = false
	if err := store.Remove(context.Background(), reuse); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, created.Key)); err != nil {
		t.Fatal("reused object removed", err)
	}
	if err := store.Remove(context.Background(), created); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(context.Background(), created); err != nil {
		t.Fatal("remove not idempotent", err)
	}
	entries, err := os.ReadDir(filepath.Join(store.Root, "avatar"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files leaked: %v %v", entries, err)
	}
}
func TestFileBoundaryAndExportAuthorization(t *testing.T) {
	cfg := config(t)
	s := files.New(nil, cfg, nil, nil)
	root := filepath.Join(cfg.DataDir, "files")
	if err := os.MkdirAll(filepath.Join(root, "defaults"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "defaults", "avatar.svg"), []byte("<svg/>"), 0600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(cfg.DataDir, "blog.db")
	if err := os.WriteFile(secret, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cfg.DataDir, filepath.Join(root, "articles")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(context.Background(), "articles", "a.png", "image/png", bytes.NewReader(pngData(t))); err == nil {
		t.Fatal("write followed scope symlink")
	}
	if err := os.Symlink(secret, filepath.Join(root, "defaults", "cover.svg")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/files/../blog.db", "/files/articles/blog.db", "/files/defaults/cover.svg", "/files/defaults/", "/files/defaults/no.svg", "/files/defaults/%00.svg", "/files/defaults/%2e%2e/blog.db"} {
		w := httptest.NewRecorder()
		s.ServeFile(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 403 && w.Code != 404 {
			t.Fatalf("unsafe path %q: %d %q", p, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Fatal("secret leaked")
		}
	}
	w := httptest.NewRecorder()
	s.ServeFile(w, httptest.NewRequest("GET", "/files/defaults/avatar.svg", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatal("trusted bootstrap SVG unavailable")
	}
	export, err := s.Put(context.Background(), "exports", "中文 标题.md", "text/markdown", strings.NewReader("# private article\n"))
	if err != nil {
		t.Fatal(err)
	}
	routes := []platform.Route{}
	for _, method := range []string{"GET", "HEAD"} {
		routes = append(routes, platform.Route{Spec: platform.RouteSpec{Method: method, Path: "/files/{path...}", Operation: "ServeFile", Policy: "public"}, Raw: http.HandlerFunc(s.ServeFile)})
	}
	allow := false
	var authorized bool
	router, err := platform.NewRouter(routes, platform.Callbacks{Resolve: func(r *http.Request) (platform.Actor, error) {
		cookie, err := r.Cookie(platform.TokenCookie)
		return platform.Actor{LoggedIn: err == nil && cookie.Value == "session", UserInfoID: 1}, nil
	}, Authorize: func(_ context.Context, _ platform.Actor, method, path string) error {
		authorized = method == "POST" && path == "/admin/articles/export"
		if !allow {
			return &platform.Error{Code: platform.CodeFail, Message: "denied"}
		}
		return nil
	}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, logged := range []bool{false, true} {
		req := httptest.NewRequest("GET", export.URL, nil)
		if logged {
			req.AddCookie(&http.Cookie{Name: platform.TokenCookie, Value: "session"})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if strings.Contains(w.Body.String(), "private article") {
			t.Fatal("private export leaked")
		}
	}
	if !authorized {
		t.Fatal("export resource not checked")
	}
	allow = true
	req := httptest.NewRequest("GET", export.URL, nil)
	req.AddCookie(&http.Cookie{Name: platform.TokenCookie, Value: "session"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	disposition, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
	if w.Code != 200 || w.Body.String() != "# private article\n" || err != nil || disposition != "attachment" || params["filename"] != "中文 标题.md" || !strings.Contains(w.Header().Get("Content-Disposition"), "filename*=") {
		t.Fatalf("export: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
}
func TestUploadEndpointsReturnOriginalStringAndEnforceMultipart(t *testing.T) {
	cfg := config(t)
	s := files.New(nil, cfg, nil, nil)
	routes := s.Routes()
	if len(routes) != 4 {
		t.Fatalf("route count %d", len(routes))
	}
	router, err := platform.NewRouter(routes, platform.Callbacks{Resolve: func(*http.Request) (platform.Actor, error) { return platform.Actor{LoggedIn: true, UserInfoID: 1}, nil }, Authorize: func(context.Context, platform.Actor, string, string) error { return nil }}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range routes {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, err := mw.CreateFormFile("file", "image.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(pngData(t))
		_ = mw.Close()
		req := httptest.NewRequest("POST", route.Spec.Path, &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var result platform.Result
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.Flag {
			t.Fatalf("upload failed: %s", w.Body.String())
		}
		address, ok := result.Data.(string)
		if !ok {
			t.Fatalf("changed string result: %v", result.Data)
		}
		u, err := url.Parse(address)
		if err != nil || !strings.HasPrefix(u.Path, "/files/") {
			t.Fatal("invalid URL")
		}
	}
	if _, err := s.SaveArticleImages(context.Background(), platform.Input{}); err == nil {
		t.Fatal("missing multipart accepted")
	}
}
func TestDiscardProtectsReferencesAndReportsCleanupFailure(t *testing.T) {
	cfg := config(t)
	db := fileDB(t)
	s := files.New(db, cfg, nil, nil)
	data := pngData(t)
	f, err := s.Put(context.Background(), "avatar", "a.png", "image/png", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO tb_user_info(nickname,avatar,create_time) VALUES('name',?,'2026-01-01')", f.URL); err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "files", f.Key)); err != nil {
		t.Fatal("referenced file deleted")
	}
	fresh, err := s.Put(context.Background(), "photos", "a.png", "image/png", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "files", fresh.Key)); !os.IsNotExist(err) {
		t.Fatal("orphan still exists", err)
	}
	again, err := s.Put(context.Background(), "talks", "a.png", "image/png", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := s.Discard(context.Background(), again); err == nil {
		t.Fatal("failed reference check hidden")
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "files", again.Key)); err != nil {
		t.Fatal("unverified object removed")
	}
}

var _ io.Reader = endlessReader{}

// The small Go x/image BSD-licensed gopher-doc WebP fixture exercises its actual decoder.
func TestFilesDecodeWebPAndRejectCorruption(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString("UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==")
	if err != nil {
		t.Fatal(err)
	}
	s := files.New(nil, config(t), nil, nil)
	f, err := s.Put(context.Background(), "articles", "gopher.webp", "image/webp", bytes.NewReader(data))
	if err != nil || !f.Created {
		t.Fatalf("valid WebP: %+v %v", f, err)
	}
	w := httptest.NewRecorder()
	s.ServeFile(w, httptest.NewRequest("GET", f.URL, nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/webp" || !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatal("WebP unavailable")
	}
	if _, err := s.Put(context.Background(), "articles", "broken.webp", "image/webp", bytes.NewReader(data[:len(data)/2])); err == nil {
		t.Fatal("corrupt WebP accepted")
	}
}

func TestFilesAcceptJPEGGIFAndMP3WithoutID3(t *testing.T) {
	s := files.New(nil, config(t), nil, nil)
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for _, format := range []string{"jpeg", "gif"} {
		var data bytes.Buffer
		var err error
		if format == "jpeg" {
			err = jpeg.Encode(&data, img, nil)
		} else {
			err = gif.Encode(&data, img, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		if f, err := s.Put(context.Background(), "photos", "photo."+format, "image/"+format, bytes.NewReader(data.Bytes())); err != nil || !f.Created {
			t.Fatalf("%s failed: %+v %v", format, f, err)
		}
	}
	// Two complete MPEG-1 Layer III frames: 128 kbps, 44.1 kHz, no optional ID3 tag.
	mp3 := make([]byte, 834)
	copy(mp3, []byte{0xff, 0xfb, 0x90, 0})
	copy(mp3[417:], []byte{0xff, 0xfb, 0x90, 0})
	if f, err := s.Put(context.Background(), "voice", "audio.mp3", "audio/mpeg", bytes.NewReader(mp3)); err != nil || !f.Created {
		t.Fatalf("MP3 rejected: %+v %v", f, err)
	}
}
func TestDiscardProtectsOtherOwnersAndPendingReuse(t *testing.T) {
	cfg := config(t)
	db := fileDB(t)
	s := files.New(db, cfg, nil, nil)
	data := pngData(t)
	file, err := s.Put(context.Background(), "photos", "a.png", "image/png", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO tb_friend_link(link_name,link_avatar,link_address,link_intro,create_time) VALUES('friend',?,'https://example.com','','2026-01-01')", file.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "files", file.Key)); err != nil {
		t.Fatal("friend link file deleted", err)
	}
	pending, err := s.Put(context.Background(), "talks", "a.png", "image/png", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	reused, err := s.Put(context.Background(), "talks", "b.png", "image/png", bytes.NewReader(data))
	if err != nil || reused.Created {
		t.Fatal("reuse failed", err)
	}
	if err := s.Discard(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "files", pending.Key)); err != nil {
		t.Fatal("concurrent owner's pending file deleted", err)
	}
}
func TestLocalStoreRejectsCorruptExistingObject(t *testing.T) {
	cfg := config(t)
	s := files.New(nil, cfg, nil, nil)
	data := pngData(t)
	f, err := s.Put(context.Background(), "articles", "a.png", "image/png", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "files", f.Key), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(context.Background(), "articles", "a.png", "image/png", bytes.NewReader(data)); err == nil {
		t.Fatal("corrupt existing object reported as a successful upload")
	}
}

func TestImageDimensionsAndFileRootSymlinkAreRejected(t *testing.T) {
	data := pngData(t)
	// Keep IHDR checksum valid so DecodeConfig sees the oversized dimensions.
	binary.BigEndian.PutUint32(data[16:20], 20000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	cfg := config(t)
	s := files.New(nil, cfg, nil, nil)
	if _, err := s.Put(context.Background(), "articles", "large.png", "image/png", bytes.NewReader(data)); err == nil {
		t.Fatal("oversized decoded image accepted")
	}
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(cfg.DataDir, "files")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(context.Background(), "articles", "a.png", "image/png", bytes.NewReader(pngData(t))); err == nil {
		t.Fatal("public root symlink accepted")
	}
	entries, err := os.ReadDir(external)
	if err != nil || len(entries) != 0 {
		t.Fatal("write escaped public root")
	}
}

func TestFrontendCompressedPNGUsesActualJPEGFormat(t *testing.T) {
	// image-conversion 2.1.1 defaults to JPEG encoding but retains the original
	// Blob MIME; Article.vue then wraps those bytes with the original PNG name.
	var compressed bytes.Buffer
	mustJPEG := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	if err := jpeg.Encode(&compressed, mustJPEG, nil); err != nil {
		t.Fatal(err)
	}
	cfg := config(t)
	service := files.New(nil, cfg, nil, nil)
	file, err := service.Put(context.Background(), "articles", "original.png", "image/png", bytes.NewReader(compressed.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	expected := fmt.Sprintf("articles/%x.jpg", sha256.Sum256(compressed.Bytes()))
	if file.Key != expected || !file.Created {
		t.Fatalf("compressed image not normalized: %+v", file)
	}
	response := httptest.NewRecorder()
	service.ServeFile(response, httptest.NewRequest("GET", file.URL, nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(response.Body.Bytes(), compressed.Bytes()) {
		t.Fatalf("JPEG not served by its actual format: %d %v", response.Code, response.Header())
	}
	// The same old filename also remains compatible if the browser reports
	// the encoder's JPEG MIME instead of retaining the original PNG MIME.
	correctMIME, err := service.Put(context.Background(), "articles", "original.png", "image/jpeg", bytes.NewReader(compressed.Bytes()))
	if err != nil || correctMIME.Created || correctMIME.Key != file.Key {
		t.Fatalf("allowed raster MIME mismatch not normalized: %+v %v", correctMIME, err)
	}
	duplicate, err := service.Put(context.Background(), "articles", "actual.jpeg", "image/jpeg", bytes.NewReader(compressed.Bytes()))
	if err != nil || duplicate.Created || duplicate.Key != file.Key {
		t.Fatalf("normalization broke content dedup: %+v %v", duplicate, err)
	}
}
