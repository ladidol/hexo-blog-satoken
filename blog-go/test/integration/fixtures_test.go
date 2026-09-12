package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math/big"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	stdmail "net/mail"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"blog-go/internal/analytics"
	"blog-go/internal/app"
	"blog-go/internal/article"
	"blog-go/internal/catalog"
	"blog-go/internal/chat"
	"blog-go/internal/community"
	"blog-go/internal/files"
	"blog-go/internal/identity"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
	"github.com/gin-gonic/gin"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate integration tests")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

type fixture struct {
	app                *app.App
	server             *httptest.Server
	cfg                platform.Config
	guest, admin, user *http.Client
	adminPassword      string
	adminUsername      string
	hits               map[string]int
}
type response struct {
	status int
	result platform.Result
	raw    []byte
	header http.Header
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	must(t, err)
	return &http.Client{Jar: jar, Timeout: 15 * time.Second}
}
func newFixture(t *testing.T, configure ...func(*platform.Config)) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	must(t, err)
	f := &fixture{server: server, hits: make(map[string]int), guest: newClient(t), admin: newClient(t), user: newClient(t)}
	f.cfg = platform.Config{Addr: "127.0.0.1:0", DataDir: t.TempDir(), PublicURL: "http://localhost:" + port, FrontendURL: "http://localhost:8080", AllowedOrigins: []string{"http://localhost:8080", "http://localhost:8081"}, TimeZone: time.FixedZone("Asia/Shanghai", 8*3600), Values: map[string]string{"BLOG_UPLOAD_MODE": "local", "BLOG_IP_ENABLED": "false"}}
	for _, option := range configure {
		option(&f.cfg)
	}
	f.app, err = app.New(context.Background(), f.cfg)
	must(t, err)
	t.Cleanup(func() {
		server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.app.Chat.Close(ctx); err != nil {
			t.Error(err)
		}
		if err := f.app.DB.Close(); err != nil {
			t.Error(err)
		}
	})
	account, err := os.ReadFile(filepath.Join(f.cfg.DataDir, "initial-admin.txt"))
	must(t, err)
	for _, line := range strings.Split(string(account), "\n") {
		if strings.HasPrefix(line, "username=") {
			f.adminUsername = strings.TrimPrefix(line, "username=")
		}
		if strings.HasPrefix(line, "password=") {
			f.adminPassword = strings.TrimPrefix(line, "password=")
		}
	}
	hash, err := identity.HashPassword("fixture-password")
	must(t, err)
	execSQL(t, f.app.DB, `INSERT INTO tb_user_info(id,nickname,avatar,email,create_time) VALUES(11,'Fixture reader','/files/defaults/avatar.svg','reader@example.test','2026-09-12T16:00:00');
		INSERT INTO tb_user_auth(id,user_info_id,username,password,login_type,create_time) VALUES(101,11,'reader@example.test','',1,'2026-09-12T16:00:00');
		INSERT INTO tb_user_role(user_id,role_id) VALUES(11,2)`)
	execSQL(t, f.app.DB, "UPDATE tb_user_auth SET password=? WHERE id=101", hash)
	server.Config.Handler = f.app.Engine
	server.Start()
	f.ok(t, f.admin, "POST", "/login", url.Values{"username": {f.adminUsername}, "password": {f.adminPassword}})
	f.ok(t, f.user, "POST", "/login", url.Values{"username": {"reader@example.test"}, "password": {"fixture-password"}})
	return f
}
func execSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	_, err := db.Exec(q, args...)
	must(t, err)
}
func rowID(t *testing.T, db *sql.DB, q string, args ...any) int64 {
	t.Helper()
	var id int64
	must(t, db.QueryRow(q, args...).Scan(&id))
	return id
}
func specs() []platform.RouteSpec {
	var all []platform.RouteSpec
	for _, group := range [][]platform.RouteSpec{identity.RouteSpecs, article.RouteSpecs, catalog.RouteSpecs, community.RouteSpecs, media.RouteSpecs, site.RouteSpecs, analytics.RouteSpecs, files.RouteSpecs, chat.RouteSpecs} {
		all = append(all, group...)
	}
	return all
}
func routeMatches(pattern, path string) bool {
	a, b := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(a) != len(b) {
		return false
	}
	for i, segment := range a {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			if b[i] == "" {
				return false
			}
			continue
		}
		if segment != b[i] {
			return false
		}
	}
	return true
}
func (f *fixture) record(method, path string) {
	parsed, _ := url.Parse(path)
	path = parsed.Path
	for _, spec := range specs() {
		if (spec.Method == method || spec.Method == "ANY") && spec.Path == path {
			f.hits[spec.Method+" "+spec.Path]++
			return
		}
	}
	for _, spec := range specs() {
		if (spec.Method == method || spec.Method == "ANY") && routeMatches(spec.Path, path) {
			f.hits[spec.Method+" "+spec.Path]++
			return
		}
	}
}
func (f *fixture) call(t *testing.T, client *http.Client, method, path string, body any) response {
	t.Helper()
	var reader io.Reader
	contentType := ""
	switch value := body.(type) {
	case nil:
	case url.Values:
		reader = strings.NewReader(value.Encode())
		contentType = "application/x-www-form-urlencoded"
	case []byte:
		reader = bytes.NewReader(value)
		contentType = "application/json"
	default:
		data, err := json.Marshal(value)
		must(t, err)
		reader = bytes.NewReader(data)
		contentType = "application/json"
	}
	r, err := http.NewRequest(method, f.cfg.PublicURL+path, reader)
	must(t, err)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return f.send(t, client, r)
}
func (f *fixture) send(t *testing.T, client *http.Client, request *http.Request) response {
	t.Helper()
	r, err := client.Do(request)
	must(t, err)
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	must(t, err)
	out := response{status: r.StatusCode, raw: data, header: r.Header}
	if err := json.Unmarshal(data, &out.result); err != nil {
		t.Fatalf("%s %s did not return Result: status=%d body=%s", request.Method, request.URL.Path, r.StatusCode, data)
	}
	f.record(request.Method, request.URL.RequestURI())
	return out
}
func (f *fixture) ok(t *testing.T, client *http.Client, method, path string, body any) any {
	t.Helper()
	r := f.call(t, client, method, path, body)
	if r.status != 200 || !r.result.Flag || r.result.Code != platform.CodeOK {
		t.Fatalf("%s %s failed: status=%d result=%#v", method, path, r.status, r.result)
	}
	return r.result.Data
}
func (f *fixture) fail(t *testing.T, client *http.Client, method, path string, body any, code int) {
	t.Helper()
	r := f.call(t, client, method, path, body)
	if r.result.Flag || r.result.Code != code {
		t.Fatalf("%s %s error=%d flag=%v want %d", method, path, r.result.Code, r.result.Flag, code)
	}
}
func object(t *testing.T, value any) map[string]any {
	t.Helper()
	out, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T", value)
	}
	return out
}
func list(t *testing.T, value any) []any {
	t.Helper()
	out, ok := value.([]any)
	if !ok {
		t.Fatalf("expected non-null array, got %T", value)
	}
	return out
}
func page(t *testing.T, value any) map[string]any {
	t.Helper()
	out := object(t, value)
	list(t, out["recordList"])
	if _, ok := out["count"].(float64); !ok {
		t.Fatalf("page count not numeric: %#v", out)
	}
	return out
}
func pathID(pattern string, id int64) string { return fmt.Sprintf(pattern, id) }

var providerCertificate tls.Certificate

func TestMain(m *testing.M) {
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "BLOG_") {
			os.Unsetenv(key)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "integration-only"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	providerCertificate = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	os.Setenv("GODEBUG", os.Getenv("GODEBUG")+",x509usefallbackroots=1")
	x509.SetFallbackRoots(roots)
	os.Exit(m.Run())
}

type localTransport struct {
	base   *http.Transport
	target *url.URL
}

func (s localTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	u := *r.URL
	clone.URL = &u
	host := r.URL.Hostname()
	if host == "graph.qq.com" || host == "api.weibo.com" || host == "oss.fixture.test" || strings.HasSuffix(host, ".oss.fixture.test") || host == "cdn.fixture.test" {
		clone.URL.Scheme = s.target.Scheme
		clone.URL.Host = s.target.Host
		clone.Header = clone.Header.Clone()
		clone.Header.Set("X-Fixture-Provider", host)
	} else if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return nil, fmt.Errorf("integration blocked non-local destination")
	}
	return s.base.RoundTrip(clone)
}

type providers struct {
	http     *httptest.Server
	smtp     net.Listener
	messages chan string
	reject   atomic.Bool
	mu       sync.Mutex
	objects  map[string][]byte
	types    map[string]string
	wg       sync.WaitGroup
}

func newProviders(t *testing.T) *providers {
	t.Helper()
	p := &providers{messages: make(chan string, 64), objects: make(map[string][]byte), types: make(map[string]string)}
	p.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider := r.Header.Get("X-Fixture-Provider")
		if strings.Contains(provider, "oss.fixture.test") || provider == "cdn.fixture.test" {
			key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/integration-bucket"), "/")
			p.mu.Lock()
			defer p.mu.Unlock()
			data, exists := p.objects[key]
			switch r.Method {
			case "HEAD":
				if !exists {
					w.Header().Set("x-oss-error-code", "NoSuchKey")
					w.WriteHeader(404)
					return
				}
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			case "PUT":
				if !strings.HasPrefix(r.Header.Get("Authorization"), "OSS4-HMAC-SHA256 ") || r.Header.Get("x-oss-forbid-overwrite") != "true" {
					w.WriteHeader(403)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(400)
					return
				}
				p.objects[key] = body
				p.types[key] = r.Header.Get("Content-Type")
			case "GET":
				if !exists {
					w.WriteHeader(404)
					return
				}
				w.Header().Set("Content-Type", p.types[key])
				w.Write(data)
			case "DELETE":
				delete(p.objects, key)
				delete(p.types, key)
				w.WriteHeader(204)
			default:
				w.WriteHeader(405)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth2.0/me":
			if r.URL.Query().Get("access_token") != "approved-qq" {
				io.WriteString(w, `{"error":1001}`)
				return
			}
			io.WriteString(w, `{"client_id":"integration-qq","openid":"approved-open-id"}`)
		case "/user/get_user_info":
			io.WriteString(w, `{"ret":0,"nickname":"Controlled QQ","figureurl_qq_2":"https://example.test/qq.png"}`)
		case "/oauth2/access_token":
			r.ParseForm()
			if r.Form.Get("code") != "approved-code" {
				io.WriteString(w, `{"error":"rejected"}`)
				return
			}
			io.WriteString(w, `{"access_token":"controlled-weibo-token","uid":"42"}`)
		case "/oauth2/get_token_info":
			io.WriteString(w, `{"appkey":"123","uid":"42","expire_in":3600}`)
		case "/2/users/show.json":
			io.WriteString(w, `{"idstr":"42","screen_name":"Controlled Weibo","avatar_hd":"https://example.test/weibo.png"}`)
		case "/ip":
			io.WriteString(w, `{"status":0,"data":[{"location":"测试地区"}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	var err error
	p.smtp, err = tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{providerCertificate}, MinVersion: tls.VersionTLS12})
	must(t, err)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			conn, err := p.smtp.Accept()
			if err != nil {
				return
			}
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				wire := textproto.NewConn(conn)
				wire.PrintfLine("220 integration SMTP")
				for {
					line, err := wire.ReadLine()
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
						wire.PrintfLine("250 integration")
					case strings.HasPrefix(line, "MAIL FROM"), strings.HasPrefix(line, "RCPT TO"):
						wire.PrintfLine("250 OK")
					case line == "DATA":
						wire.PrintfLine("354 Send content")
						body, err := wire.ReadDotBytes()
						if err != nil {
							return
						}
						p.messages <- string(body)
						if p.reject.Load() {
							wire.PrintfLine("550 Controlled rejection")
						} else {
							wire.PrintfLine("250 Accepted")
						}
					case line == "QUIT":
						wire.PrintfLine("221 Bye")
						return
					default:
						wire.PrintfLine("250 OK")
					}
				}
			}()
		}
	}()
	previous := http.DefaultTransport
	base := &http.Transport{Proxy: nil}
	target, err := url.Parse(p.http.URL)
	must(t, err)
	http.DefaultTransport = localTransport{base: base, target: target}
	t.Cleanup(func() {
		http.DefaultTransport = previous
		base.CloseIdleConnections()
		p.smtp.Close()
		p.http.Close()
		p.wg.Wait()
	})
	return p
}
func (p *providers) configure(cfg *platform.Config) {
	_, port, _ := net.SplitHostPort(p.smtp.Addr().String())
	for k, v := range map[string]string{"BLOG_SMTP_HOST": "127.0.0.1", "BLOG_SMTP_PORT": port, "BLOG_SMTP_TLS_MODE": "tls", "BLOG_SMTP_FROM": "integration@example.test", "BLOG_QQ_APP_ID": "integration-qq", "BLOG_WEIBO_APP_KEY": "123", "BLOG_WEIBO_APP_SECRET": "controlled-secret", "BLOG_WEIBO_REDIRECT_URI": "http://localhost/callback", "BLOG_IP_ENABLED": "true", "BLOG_IP_ENDPOINT": p.http.URL + "/ip"} {
		cfg.Values[k] = v
	}
}
func (p *providers) code(t *testing.T) string {
	t.Helper()
	select {
	case raw := <-p.messages:
		message, err := stdmail.ReadMessage(strings.NewReader(raw))
		must(t, err)
		body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
		must(t, err)
		code := regexp.MustCompile(`[0-9]{6}`).FindString(string(body))
		if code == "" {
			t.Fatal("controlled SMTP did not receive a code")
		}
		return code
	case <-time.After(3 * time.Second):
		t.Fatal("controlled SMTP did not receive mail")
		return ""
	}
}
func imageData(t *testing.T, jpegBytes bool) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{R: 220, G: 70, B: 20, A: 255})
	var out bytes.Buffer
	var err error
	if jpegBytes {
		err = jpeg.Encode(&out, im, nil)
	} else {
		err = png.Encode(&out, im)
	}
	must(t, err)
	return out.Bytes()
}
func wavData() []byte {
	data := make([]byte, 48)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], 40)
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 4)
	return data
}
func (f *fixture) upload(t *testing.T, client *http.Client, path, name, mime string, data []byte, fields map[string]string) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreatePart(textproto.MIMEHeader{"Content-Disposition": {fmt.Sprintf(`form-data; name="file"; filename="%s"`, name)}, "Content-Type": {mime}})
	must(t, err)
	_, err = part.Write(data)
	must(t, err)
	for k, v := range fields {
		must(t, form.WriteField(k, v))
	}
	must(t, form.Close())
	r, err := http.NewRequest("POST", f.cfg.PublicURL+path, &body)
	must(t, err)
	r.Header.Set("Content-Type", form.FormDataContentType())
	return f.send(t, client, r)
}
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
func TestFrontendPreparationPreservesSourceAndLockedPackages(t *testing.T) {
	source, output := filepath.Join(t.TempDir(), "source"), filepath.Join(t.TempDir(), "copies")
	lock := `{"lockfileVersion":1,"dependencies":{"@scope/example":{"version":"1.2.3","integrity":"sha512-fixed","resolved":"https://registry.npm.taobao.org/@scope/example/download/@scope/example-1.2.3.tgz?cache=0"},"plain":{"version":"2.0.0","integrity":"sha512-also-fixed","resolved":"https://registry.npmmirror.com/plain/-/plain-2.0.0.tgz"}}}`
	for _, name := range []string{"blog", "admin"} {
		dir := filepath.Join(source, name)
		write(t, filepath.Join(dir, "package.json"), `{"name":"fixture","scripts":{"serve":"vue-cli-service serve","build":"vue-cli-service build"}}`)
		write(t, filepath.Join(dir, "package-lock.json"), lock)
		write(t, filepath.Join(dir, "vue.config.js"), `module.exports={devServer:{proxy:{"/api":{target:"http://localhost:8888",pathRewrite:{"^/api":""}}}}};`)
		write(t, filepath.Join(dir, "src/main.js"), "// original application\n")
	}
	cmd := exec.Command("python3", filepath.Join(projectRoot(t), "scripts/prepare-frontends.py"), "--source-root", source, "--output", output, "--skip-install")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("frontend preparation failed: %v\n%s", err, out)
	}
	for _, name := range []string{"blog", "admin"} {
		original, err := os.ReadFile(filepath.Join(source, name, "package-lock.json"))
		if err != nil || string(original) != lock {
			t.Fatal("original lock changed")
		}
		data, err := os.ReadFile(filepath.Join(output, name, "package-lock.json"))
		if err != nil {
			t.Fatal(err)
		}
		var copied struct {
			Dependencies map[string]struct{ Version, Integrity, Resolved string }
		}
		if err := json.Unmarshal(data, &copied); err != nil {
			t.Fatal(err)
		}
		scoped := copied.Dependencies["@scope/example"]
		if scoped.Version != "1.2.3" || scoped.Integrity != "sha512-fixed" || scoped.Resolved != "https://registry.npmjs.org/@scope/example/-/example-1.2.3.tgz" {
			t.Fatalf("changed package identity or wrong registry URL: %#v", scoped)
		}
		if copied.Dependencies["plain"].Resolved != "https://registry.npmjs.org/plain/-/plain-2.0.0.tgz" {
			t.Fatal("npmmirror URL not normalized")
		}
	}
	write(t, filepath.Join(output, "blog/src/main.js"), "// user edited copy\n")
	cmd = exec.Command("python3", filepath.Join(projectRoot(t), "scripts/prepare-frontends.py"), "--source-root", source, "--output", output, "--skip-install")
	if _, err := cmd.CombinedOutput(); err == nil {
		t.Fatal("modified copy should require a different output directory")
	}
	data, _ := os.ReadFile(filepath.Join(output, "blog/src/main.js"))
	if string(data) != "// user edited copy\n" {
		t.Fatal("modified copy was overwritten")
	}
}
