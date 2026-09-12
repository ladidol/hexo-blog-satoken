package platform_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"blog-go/internal/analytics"
	"blog-go/internal/article"
	"blog-go/internal/catalog"
	"blog-go/internal/chat"
	"blog-go/internal/community"
	"blog-go/internal/files"
	"blog-go/internal/identity"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
)

func config() platform.Config {
	return platform.Config{
		Addr: "127.0.0.1:0", PublicURL: "http://localhost:8888",
		FrontendURL:    "http://localhost:8080",
		AllowedOrigins: []string{"http://localhost:8080", "http://localhost:8081"},
		TimeZone:       time.FixedZone("Asia/Shanghai", 8*60*60),
		Values:         map[string]string{"BLOG_UPLOAD_MODE": "local"},
	}
}

func router(t *testing.T, routes []platform.Route, callbacks platform.Callbacks) *gin.Engine {
	t.Helper()
	engine, err := platform.NewRouter(routes, callbacks, config())
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func call(engine http.Handler, method, target, contentType string, body io.Reader) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, body)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func result(t *testing.T, response *httptest.ResponseRecorder, status, code int) platform.Result {
	t.Helper()
	if response.Code != status {
		t.Fatalf("HTTP %d, want %d: %s", response.Code, status, response.Body)
	}
	var body platform.Result
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("not a Result: %s: %v", response.Body, err)
	}
	if body.Code != code || body.Flag != (code == platform.CodeOK) {
		t.Fatalf("Result = %+v, want code %d", body, code)
	}
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatal("missing JSON content type")
	}
	return body
}

func testRoute(method, path, policy string, endpoint platform.Endpoint) platform.Route {
	return platform.Route{
		Spec:   platform.RouteSpec{Method: method, Path: path, Operation: "TestOperation", Policy: policy},
		Handle: endpoint,
	}
}

func TestGinRegistersEveryBusinessRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groups := [][]platform.RouteSpec{
		identity.RouteSpecs, article.RouteSpecs, catalog.RouteSpecs, community.RouteSpecs,
		media.RouteSpecs, site.RouteSpecs, analytics.RouteSpecs, files.RouteSpecs, chat.RouteSpecs,
	}
	var routes []platform.Route
	var specs []platform.RouteSpec
	for _, group := range groups {
		for _, spec := range group {
			spec := spec
			specs = append(specs, spec)
			routes = append(routes, platform.Route{Spec: spec, Handle: func(_ context.Context, in platform.Input) (any, error) {
				return platform.Object{"operation": spec.Operation, "params": in.Params}, nil
			}})
		}
	}
	if len(routes) != 108 {
		t.Fatalf("expected 108 frozen business routes, got %d", len(routes))
	}
	engine := router(t, routes, platform.Callbacks{
		Resolve: func(*http.Request) (platform.Actor, error) {
			return platform.Actor{LoggedIn: true, UserInfoID: 7}, nil
		},
		Authorize: func(context.Context, platform.Actor, string, string) error { return nil },
		Audit:     func(context.Context, platform.AuditEvent) error { return nil },
	})
	for _, spec := range specs {
		method := spec.Method
		if method == "ANY" {
			method = http.MethodPost
		}
		target := spec.Path
		for strings.Contains(target, "{") {
			start := strings.Index(target, "{")
			end := strings.Index(target, "}")
			target = target[:start] + "42" + target[end+1:]
		}
		t.Run(method+" "+target, func(t *testing.T) {
			body := result(t, call(engine, method, target, "", nil), http.StatusOK, platform.CodeOK)
			if body.Data.(map[string]any)["operation"] != spec.Operation {
				t.Fatalf("wrong matched operation: %v", body.Data)
			}
		})
	}
	if engine.RedirectTrailingSlash || engine.RedirectFixedPath || !engine.HandleMethodNotAllowed ||
		engine.MaxMultipartMemory != 8<<20 {
		t.Fatal("Gin transport configuration changed")
	}
	result(t, call(engine, "GET", "/articles/", "", nil), 404, platform.CodeFail)
	result(t, call(engine, "PATCH", "/articles", "", nil), 405, platform.CodeFail)
	result(t, call(engine, "GET", "/not-registered", "", nil), 404, platform.CodeFail)
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "HEAD", "OPTIONS", "DELETE", "CONNECT", "TRACE"} {
		response := call(engine, method, "/logout", "", nil)
		if response.Code != 200 {
			t.Fatalf("logout %s: %d", method, response.Code)
		}
	}
}

func TestGinRejectsInvalidRegistration(t *testing.T) {
	pass := func(context.Context, platform.Input) (any, error) { return nil, nil }
	for _, path := range []string{"", "relative", "/a/{id", "/a/{}", "/a/{bad-name}", "/a/{rest...}/tail", "/a/:id", "/a/*path"} {
		t.Run(path, func(t *testing.T) {
			if _, err := platform.GinPath(path); err == nil {
				t.Fatalf("accepted invalid contract path %q", path)
			}
		})
	}
	for contract, want := range map[string]string{
		"/": "/", "/articles/{articleId}": "/articles/:articleId", "/files/{path...}": "/files/*path",
	} {
		got, err := platform.GinPath(contract)
		if err != nil || got != want {
			t.Fatalf("%s => %q, %v", contract, got, err)
		}
	}
	for _, routes := range [][]platform.Route{
		{testRoute("GET", "/a", "unknown", pass)},
		{testRoute("GET", "/a", "public", nil)},
		{testRoute("BAD METHOD", "/a", "public", pass)},
		{testRoute("GET", "/a", "public", pass), testRoute("GET", "/a", "public", pass)},
		{testRoute("GET", "/a/{id}", "public", pass), testRoute("GET", "/a/{other}", "public", pass)},
	} {
		if engine, err := platform.NewRouter(routes, platform.Callbacks{}, config()); err == nil || engine != nil {
			t.Fatalf("partial/invalid router returned: engine=%v err=%v", engine, err)
		}
	}
}

func TestBindingKeepsOriginalFormsAndValidationEnvelope(t *testing.T) {
	var seen platform.Input
	engine := router(t, []platform.Route{testRoute("POST", "/input/{id}", "public",
		func(_ context.Context, in platform.Input) (any, error) { seen = in; return nil, nil })}, platform.Callbacks{})
	response := call(engine, "POST", "/input/12?keywords=hello&size=7", "application/x-www-form-urlencoded",
		strings.NewReader(url.Values{"username": {"local-admin"}, "password": {"redacted-test-value"}}.Encode()))
	result(t, response, 200, platform.CodeOK)
	if seen.Params["id"] != "12" || seen.Query.Get("current") != "1" || seen.Query.Get("size") != "7" ||
		seen.Form.Get("username") != "local-admin" || seen.Form.Get("password") != "redacted-test-value" {
		t.Fatalf("binding mismatch: params=%v query=%v", seen.Params, seen.Query)
	}
	for _, body := range []string{"{\"id\":12}", "[1,2,3]"} {
		response = call(engine, "POST", "/input/12", "application/json", strings.NewReader(body))
		result(t, response, 200, platform.CodeOK)
		if string(seen.Body) != body {
			t.Fatalf("JSON changed: %s", seen.Body)
		}
	}
	for _, body := range []string{"{", "null", "\"scalar\"", "{} {}", "[1,]", ""} {
		response = call(engine, "POST", "/input/12", "application/json", strings.NewReader(body))
		result(t, response, 200, platform.CodeInvalid)
	}
	for _, query := range []string{"current=0", "current=-1", "current=x", "size=101", "size=0", "size=999999999999999999999999", "current=1&current=2", "bad=%zz"} {
		result(t, call(engine, "POST", "/input/12?"+query, "", nil), 200, platform.CodeInvalid)
	}
	response = call(engine, "POST", "/input/12", "application/json", strings.NewReader("{\"x\":\""+strings.Repeat("x", 2<<20)+"\"}"))
	result(t, response, 413, platform.CodeInvalid)
	request := httptest.NewRequest("POST", "/input/12", strings.NewReader("x"))
	request.ContentLength = 101 << 20
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	result(t, response, 413, platform.CodeInvalid)

	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	_ = writer.WriteField("type", "hexo")
	part, err := writer.CreateFormFile("file", "article.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, "# An article")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	response = call(engine, "POST", "/input/12", writer.FormDataContentType(), &multipartBody)
	result(t, response, 200, platform.CodeOK)
	if seen.Form.Get("type") != "hexo" || len(seen.Files) != 1 || seen.Files[0].Filename != "article.md" {
		t.Fatal("multipart field name or metadata changed")
	}
}

func TestAuthorizationAuditAndSensitiveErrors(t *testing.T) {
	allowed, authorized, executed := false, false, false
	var audit []platform.AuditEvent
	route := testRoute("PUT", "/admin/articles/{articleId}", "admin", func(_ context.Context, in platform.Input) (any, error) {
		executed = true
		if in.Actor.UserInfoID != 19 || in.IP != "192.0.2.1" {
			t.Fatal("untrusted identity/IP")
		}
		return nil, nil
	})
	route.Spec.Audit = true
	engine := router(t, []platform.Route{route}, platform.Callbacks{
		Resolve: func(*http.Request) (platform.Actor, error) {
			return platform.Actor{LoggedIn: true, AuthID: 2, UserInfoID: 19}, nil
		},
		Authorize: func(_ context.Context, actor platform.Actor, method, pattern string) error {
			authorized = true
			if method != "PUT" || pattern != "/admin/articles/{articleId}" || actor.AuthID != 2 {
				t.Fatal("noncanonical authorization")
			}
			if !allowed {
				return &platform.Error{Code: platform.CodeFail, Message: "没有权限"}
			}
			return nil
		},
		Audit: func(_ context.Context, event platform.AuditEvent) error { audit = append(audit, event); return nil },
	})
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/admin/articles/42", strings.NewReader("{\"id\":42,\"password\":\"DO-NOT-LOG\",\"code\":\"123456\",\"token\":\"secret\"}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "203.0.113.99")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)
		return recorder
	}
	result(t, request(), 200, platform.CodeFail)
	if !authorized || executed || len(audit) != 0 {
		t.Fatal("denial changed state or wrote success audit")
	}
	allowed = true
	result(t, request(), 200, platform.CodeOK)
	if !executed || len(audit) != 1 || audit[0].Code != platform.CodeOK ||
		len(audit[0].ObjectIDs) != 1 || audit[0].ObjectIDs[0] != 42 {
		t.Fatalf("missing safe audit facts: %+v", audit)
	}
	encoded, _ := json.Marshal(audit)
	if bytes.Contains(encoded, []byte("DO-NOT-LOG")) || bytes.Contains(encoded, []byte("123456")) {
		t.Fatal("sensitive audit data")
	}
	anonymous := router(t, []platform.Route{route}, platform.Callbacks{})
	result(t, call(anonymous, "PUT", "/admin/articles/42", "", nil), 200, platform.CodeFail)

	for _, mode := range []string{"error", "panic", "serialization"} {
		failing := router(t, []platform.Route{testRoute("GET", "/", "public", func(context.Context, platform.Input) (any, error) {
			switch mode {
			case "error":
				return nil, errors.New("secret-provider-token")
			case "panic":
				panic("secret-provider-token")
			default:
				return make(chan int), nil
			}
		})}, platform.Callbacks{})
		response := call(failing, "GET", "/", "", nil)
		result(t, response, 200, platform.CodeSystem)
		if strings.Contains(response.Body.String(), "secret-provider-token") {
			t.Fatal("error leaked provider data")
		}
	}
}

func TestCORSVisitorAndEmptyResults(t *testing.T) {
	engine := router(t, []platform.Route{testRoute("GET", "/", "public", func(context.Context, platform.Input) (any, error) {
		return platform.Object{"page": platform.Page{}, "items": []platform.Object(nil), "optional": nil}, nil
	})}, platform.Callbacks{})
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Origin", "http://localhost:8080")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	body := result(t, response, 200, platform.CodeOK)
	data := body.Data.(map[string]any)
	if data["items"] == nil || data["optional"] != nil || data["page"].(map[string]any)["recordList"] == nil {
		t.Fatalf("empty shape changed: %v", data)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:8080" ||
		response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("missing credential CORS")
	}
	var visitor *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == platform.VisitorCookie {
			visitor = cookie
		}
	}
	if visitor == nil {
		t.Fatal("no visitor cookie")
	}
	raw, err := base64.RawURLEncoding.DecodeString(visitor.Value)
	if err != nil || len(raw) != 32 || !visitor.HttpOnly || visitor.SameSite != http.SameSiteLaxMode ||
		visitor.Path != "/" || visitor.Secure || visitor.MaxAge != 0 {
		t.Fatal("visitor cookie is not a random protected browser-session cookie")
	}
	request = httptest.NewRequest("GET", "/", nil)
	request.AddCookie(visitor)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("valid visitor cookie changed")
	}
	request = httptest.NewRequest("OPTIONS", "/", nil)
	request.Header.Set("Origin", "http://localhost:8081")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != 204 {
		t.Fatalf("preflight: %d", response.Code)
	}
	request.Header.Set("Origin", "https://untrusted.example")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	result(t, response, 403, platform.CodeFail)
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("untrusted origin allowed")
	}
}

func TestRawFileAndWebSocketBridge(t *testing.T) {
	actor := platform.Actor{LoggedIn: true, UserInfoID: 9}
	allowExport := false
	var exportsAuthorized bool
	routes := []platform.Route{
		{Spec: platform.RouteSpec{Method: "GET", Path: "/files/{path...}", Operation: "ServeFile", Policy: "public"},
			Raw: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Context().Value(platform.Actor{}) != actor {
					t.Fatal("raw identity missing")
				}
				http.ServeContent(w, r, "sample.txt", time.Time{}, strings.NewReader("abcdef"))
			})},
		{Spec: platform.RouteSpec{Method: "GET", Path: "/websocket", Operation: "ServeWebSocket", Policy: "public"},
			Raw: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.WriteJSON(chat.Frame{Type: 6, Data: "pong"})
			})},
	}
	head := routes[0]
	head.Spec.Method = "HEAD"
	routes = append(routes, head)
	engine := router(t, routes, platform.Callbacks{
		Resolve: func(*http.Request) (platform.Actor, error) { return actor, nil },
		Authorize: func(_ context.Context, _ platform.Actor, method, pattern string) error {
			if method != "POST" || pattern != "/admin/articles/export" {
				t.Fatal("incorrect export resource")
			}
			exportsAuthorized = true
			if !allowExport {
				return &platform.Error{Code: platform.CodeFail, Message: "没有权限"}
			}
			return nil
		},
	})
	result(t, call(engine, "GET", "/files/exports/article.md", "", nil), 200, platform.CodeFail)
	if !exportsAuthorized {
		t.Fatal("exports bypassed resource authorization")
	}
	allowExport = true
	response := call(engine, "GET", "/files/exports/article.md", "", nil)
	if response.Body.String() != "abcdef" {
		t.Fatalf("raw response wrapped: %s", response.Body)
	}
	response = call(engine, "HEAD", "/files/audio/a.wav", "", nil)
	if response.Body.Len() != 0 || response.Header().Get("Content-Length") != "6" {
		t.Fatal("HEAD bridge changed")
	}
	server := httptest.NewServer(engine)
	defer server.Close()
	conn, responseHTTP, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/websocket", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = responseHTTP.Body.Close()
	var frame chat.Frame
	if err := conn.ReadJSON(&frame); err != nil || frame.Type != 6 || frame.Data != "pong" {
		t.Fatalf("Gin hijack/JSON bridge: %+v, %v", frame, err)
	}
}

func TestConfigDefaultsAndValidation(t *testing.T) {
	for _, key := range []string{"BLOG_ADDR", "BLOG_DATA_DIR", "BLOG_PUBLIC_URL", "BLOG_FRONTEND_URL", "BLOG_ALLOWED_ORIGINS", "BLOG_TIMEZONE", "BLOG_UPLOAD_MODE", "BLOG_IP_ENABLED", "BLOG_SMTP_PORT", "BLOG_SMTP_TLS_MODE"} {
		t.Setenv(key, "")
	}
	cfg, err := platform.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:8888" || cfg.DataDir != "./data" || cfg.PublicURL != "http://localhost:8888" ||
		cfg.FrontendURL != "http://localhost:8080" || cfg.TimeZone.String() != "Asia/Shanghai" ||
		cfg.Values["BLOG_UPLOAD_MODE"] != "local" || cfg.Values["BLOG_IP_ENABLED"] != "false" ||
		len(cfg.AllowedOrigins) != 2 {
		t.Fatalf("wrong defaults: addr=%s data=%s", cfg.Addr, cfg.DataDir)
	}
	for _, pair := range [][2]string{
		{"BLOG_ADDR", "not-an-address"}, {"BLOG_PUBLIC_URL", "javascript:bad"},
		{"BLOG_PUBLIC_URL", "http://user:secret@localhost:8888"},
		{"BLOG_ALLOWED_ORIGINS", "*"}, {"BLOG_ALLOWED_ORIGINS", "http://localhost:8080/path"},
		{"BLOG_TIMEZONE", "Unknown/Zone"}, {"BLOG_UPLOAD_MODE", "fake"},
		{"BLOG_IP_ENABLED", "yes"}, {"BLOG_SMTP_PORT", "70000"}, {"BLOG_SMTP_TLS_MODE", "plain"},
	} {
		t.Run(fmt.Sprint(pair), func(t *testing.T) {
			t.Setenv(pair[0], pair[1])
			if _, err := platform.LoadConfig(); err == nil {
				t.Fatalf("accepted invalid %s", pair[0])
			}
		})
	}
}

func TestEndpointCanPreserveAnExplicitSuccessMessage(t *testing.T) {
	engine := router(t, []platform.Route{testRoute("POST", "/comments", "public",
		func(context.Context, platform.Input) (any, error) {
			return platform.Result{Flag: true, Code: platform.CodeOK, Message: "评论已保存，通知未提交成功",
				Data: platform.Object{"commentId": 17, "notification": platform.Object{"status": "unknown", "deliveryId": 29}}}, nil
		})}, platform.Callbacks{})
	body := result(t, call(engine, "POST", "/comments", "", nil), 200, platform.CodeOK)
	if body.Message != "评论已保存，通知未提交成功" || body.Data.(map[string]any)["commentId"] != float64(17) {
		t.Fatalf("success envelope was nested or lost its message: %+v", body)
	}
}

func TestCommittedRawResponseNeverGetsASecondJSONBody(t *testing.T) {
	engine := router(t, []platform.Route{{
		Spec: platform.RouteSpec{Method: "GET", Path: "/raw", Operation: "Raw", Policy: "public"},
		Raw: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "already-written")
			panic("sensitive-internal-detail")
		}),
	}}, platform.Callbacks{})
	response := call(engine, "GET", "/raw", "", nil)
	if response.Body.String() != "already-written" {
		t.Fatalf("second body appended: %s", response.Body)
	}
}

var repeatedBlock = bytes.Repeat([]byte("x"), 32<<10)

type repeatedReader struct{}

func (repeatedReader) Read(output []byte) (int, error) {
	return copy(output, repeatedBlock), nil
}

func TestChunkedMultipartEnforcesLimitAndCleansTemporaryFiles(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	var accepted int64
	engine := router(t, []platform.Route{testRoute("POST", "/upload", "public", func(_ context.Context, in platform.Input) (any, error) {
		if len(in.Files) != 1 {
			t.Fatal("multipart file missing")
		}
		file, err := in.Files[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		accepted, err = io.Copy(io.Discard, file)
		return nil, err
	})}, platform.Callbacks{})
	for _, size := range []int64{9 << 20, 101 << 20} {
		body := io.MultiReader(
			strings.NewReader("--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"audio.wav\"\r\nContent-Type: audio/wav\r\n\r\n"),
			io.LimitReader(repeatedReader{}, size),
			strings.NewReader("\r\n--boundary--\r\n"),
		)
		request := httptest.NewRequest("POST", "/upload", body)
		request.ContentLength = -1
		request.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if size < 100<<20 {
			result(t, response, 200, platform.CodeOK)
			if accepted != size {
				t.Fatal("multipart content was truncated")
			}
		} else {
			result(t, response, 413, platform.CodeInvalid)
			if accepted != 9<<20 {
				t.Fatal("oversized multipart reached the endpoint")
			}
		}
		entries, err := os.ReadDir(temporary)
		if err != nil || len(entries) != 0 {
			t.Fatalf("multipart temporary files leaked: %v %v", entries, err)
		}
	}
}
