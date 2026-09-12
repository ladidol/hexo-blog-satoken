package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 用途：注入身份、权限和审计能力，避免公共协议反向依赖业务 package。
type Callbacks struct {
	Resolve   func(*http.Request) (Actor, error)
	Authorize func(context.Context, Actor, string, string) error
	Audit     func(context.Context, AuditEvent) error
}

// 用途：以 Gin 构建兼容路由、中间件和标准失败封装；注册错误在启动阶段返回。
func NewRouter(routes []Route, callbacks Callbacks, cfg Config) (engine *gin.Engine, err error) {
	// Gin rejects conflicts by panic; never return a partially registered engine.
	defer func() {
		if recover() != nil {
			engine = nil
			err = errors.New("conflicting HTTP route declarations")
		}
	}()
	engine = gin.New()
	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false
	engine.HandleMethodNotAllowed = true
	engine.MaxMultipartMemory = 8 << 20
	if err = engine.SetTrustedProxies(nil); err != nil {
		return nil, err
	}
	for _, origin := range cfg.AllowedOrigins {
		if !validHTTPURL(origin, true) {
			return nil, errors.New("invalid allowed HTTP origin")
		}
	}
	engine.Use(RecoveryMiddleware(), CORSMiddleware(cfg))
	engine.NoRoute(func(c *gin.Context) { writeFailure(c, http.StatusNotFound, CodeFail, "接口不存在") })
	engine.NoMethod(func(c *gin.Context) { writeFailure(c, http.StatusMethodNotAllowed, CodeFail, "请求方法不支持") })
	for _, route := range routes {
		spec := route.Spec
		if (route.Handle == nil) == (route.Raw == nil) || spec.Operation == "" {
			return nil, errors.New("route requires one handler and an operation")
		}
		if spec.Policy != "public" && spec.Policy != "user" && spec.Policy != "admin" {
			return nil, errors.New("unknown route access policy")
		}
		if !standardMethod(spec.Method) && spec.Method != "ANY" {
			return nil, errors.New("invalid HTTP route method")
		}
		ginPath, err := GinPath(spec.Path)
		if err != nil {
			return nil, err
		}
		handler := AdaptRoute(route, callbacks, cfg)
		if spec.Method == "ANY" {
			engine.Any(ginPath, handler)
		} else {
			engine.Handle(spec.Method, ginPath, handler)
		}
	}
	return engine, nil
}

// 用途：将唯一协议清单的 {id}/{path...} 模板转换成 Gin 的 :id/*path，保留原参数名。
func GinPath(contractPath string) (string, error) {
	if contractPath == "/" {
		return "/", nil
	}
	if !strings.HasPrefix(contractPath, "/") || strings.ContainsAny(contractPath, "?#\r\n\t :*") {
		return "", errors.New("invalid contract route path")
	}
	segments := strings.Split(contractPath[1:], "/")
	names := make(map[string]bool)
	for index, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("invalid route path segment")
		}
		if !strings.ContainsAny(segment, "{}") {
			continue
		}
		if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
			return "", errors.New("invalid route parameter")
		}
		name := segment[1 : len(segment)-1]
		prefix := ":"
		if strings.HasSuffix(name, "...") {
			name = strings.TrimSuffix(name, "...")
			prefix = "*"
			if index != len(segments)-1 {
				return "", errors.New("wildcard must be the final route segment")
			}
		}
		if !parameterName.MatchString(name) || names[name] {
			return "", errors.New("invalid or duplicate route parameter name")
		}
		names[name] = true
		segments[index] = prefix + name
	}
	return "/" + strings.Join(segments, "/"), nil
}

var parameterName = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]*$")

const (
	maxRequestBytes int64 = 100 << 20
	maxJSONBytes    int64 = 2 << 20
)

// 用途：在 Gin 边界执行身份/授权，调用既有模块方法，或承接文件/WebSocket原始响应。
func AdaptRoute(route Route, callbacks Callbacks, cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxRequestBytes {
			writeFailure(c, http.StatusRequestEntityTooLarge, CodeInvalid, "请求内容过大")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
		if route.Raw != nil && route.Spec.Policy == "public" && route.Spec.Method == http.MethodGet && route.Spec.Path == "/healthz" {
			route.Raw.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		visitor, err := ensureVisitor(c, cfg)
		if err != nil {
			writeError(c, err)
			return
		}
		var actor Actor
		if callbacks.Resolve != nil {
			actor, err = callbacks.Resolve(c.Request)
			if err != nil {
				writeError(c, err)
				return
			}
		}
		// Actor{} is a package-scoped typed key available to the frozen Raw handlers.
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), Actor{}, actor))
		admin, method, pattern := route.Spec.Policy == "admin", c.Request.Method, route.Spec.Path
		if route.Raw != nil && route.Spec.Path == "/files/{path...}" {
			key := path.Clean(strings.TrimLeft(c.Param("path"), "/"))
			if key == "exports" || strings.HasPrefix(key, "exports/") {
				admin, method, pattern = true, http.MethodPost, "/admin/articles/export"
			}
		}
		if admin || route.Spec.Policy == "user" {
			if !actor.LoggedIn || actor.UserInfoID <= 0 {
				writeFailure(c, http.StatusOK, CodeFail, "请先登录")
				return
			}
		}
		if admin {
			if callbacks.Authorize == nil {
				writeFailure(c, http.StatusOK, CodeFail, "没有操作权限")
				return
			}
			if err := callbacks.Authorize(c.Request.Context(), actor, method, pattern); err != nil {
				writeError(c, err)
				return
			}
		}
		if route.Raw != nil {
			route.Raw.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		in, err := BindInput(c, actor)
		// ParseMultipartForm can leave files even when parsing fails.
		if c.Request.MultipartForm != nil {
			defer func() {
				if err := c.Request.MultipartForm.RemoveAll(); err != nil {
					log.Printf("multipart cleanup failed: route=%s", route.Spec.Path)
				}
			}()
		}
		if err != nil {
			writeError(c, err)
			return
		}
		in.VisitorKey = visitor
		data, err := route.Handle(c.Request.Context(), in)
		if err != nil {
			writeError(c, err)
			return
		}
		if c.Writer.Written() {
			c.Abort()
			return
		}
		envelope := Result{Flag: true, Code: CodeOK, Message: "操作成功", Data: data}
		// Existing Result is also the module's escape hatch for an explicit success
		// message, e.g. a saved comment whose notification submission was uncertain.
		if explicit, ok := data.(Result); ok {
			if !explicit.Flag || explicit.Code != CodeOK {
				writeFailure(c, http.StatusOK, CodeSystem, "系统异常")
				return
			}
			envelope = explicit
		}
		envelope.Data = normalizeResult(envelope.Data)
		encoded, err := json.Marshal(envelope)
		if err != nil {
			writeError(c, err)
			return
		}
		if route.Spec.Audit && callbacks.Audit != nil {
			ids := auditIDs(in)
			if err := callbacks.Audit(c.Request.Context(), AuditEvent{
				Actor: actor, Route: route.Spec, ObjectIDs: ids, Count: len(ids),
				Code: CodeOK, IP: in.IP, At: time.Now(),
			}); err != nil {
				// Business state is already committed. An audit failure must not invite a duplicate write.
				log.Printf("audit write failed: route=%s", route.Spec.Path)
			}
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", encoded)
		c.Abort()
	}
}

// 用途：使用 ShouldBind 类接口或显式解析构造原 query/form/JSON/multipart 输入，错误由标准封装统一处理。
func BindInput(c *gin.Context, actor Actor) (Input, error) {
	in := Input{Actor: actor, Params: make(map[string]string), Form: make(url.Values), Request: c.Request, Response: c.Writer}
	invalid := func() (Input, error) { return Input{}, &Error{Code: CodeInvalid, Message: "请求参数不正确"} }
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return invalid()
	}
	in.Query = query
	for key, fallback := range map[string]string{"current": "1", "size": "10"} {
		values, supplied := in.Query[key]
		if !supplied {
			in.Query.Set(key, fallback)
			continue
		}
		if len(values) != 1 {
			return invalid()
		}
		number, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || number < 1 || (key == "size" && number > 100) {
			return invalid()
		}
	}
	for _, param := range c.Params {
		in.Params[param.Key] = param.Value
	}
	if host, _, err := net.SplitHostPort(c.Request.RemoteAddr); err == nil {
		in.IP = host
	} else {
		in.IP = c.Request.RemoteAddr
	}
	in.Browser, in.OS = userAgent(c.Request.UserAgent())
	if cookie, err := c.Request.Cookie(VisitorCookie); err == nil {
		in.VisitorKey = cookie.Value
	}
	contentType := ""
	if header := c.Request.Header.Get("Content-Type"); header != "" {
		contentType, _, err = mime.ParseMediaType(header)
		if err != nil {
			return invalid()
		}
	}
	switch contentType {
	case "application/json":
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBytes)
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return Input{}, err
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || (raw[0] != '{' && raw[0] != '[') || !json.Valid(raw) {
			return invalid()
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		if err := c.ShouldBindJSON(&in.Body); err != nil {
			return invalid()
		}
	case "application/x-www-form-urlencoded":
		if err := c.Request.ParseForm(); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return Input{}, err
			}
			return invalid()
		}
		in.Form = c.Request.PostForm
	case "multipart/form-data":
		if err := c.Request.ParseMultipartForm(8 << 20); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return Input{}, err
			}
			return invalid()
		}
		in.Form = c.Request.MultipartForm.Value
		in.Files = c.Request.MultipartForm.File["file"]
	case "":
		if c.Request.ContentLength != 0 {
			return invalid()
		}
	default:
		return invalid()
	}
	return in, nil
}

// 用途：捕获处理异常并返回无敏感数据的系统失败；不向已提交或已升级响应追加JSON。
func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				log.Printf("HTTP handler panic: method=%s route=%s", c.Request.Method, c.FullPath())
				if !c.Writer.Written() {
					writeFailure(c, http.StatusOK, CodeSystem, "系统异常")
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

// 用途：按显式origin白名单处理带凭据请求和预检，防止Gin默认设置改变跨域边界。
func CORSMiddleware(cfg Config) gin.HandlerFunc {
	allowed := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		allowed[origin] = true
	}
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin == "" {
			c.Next()
			return
		}
		c.Header("Vary", "Origin")
		if !allowed[origin] {
			writeFailure(c, http.StatusForbidden, CodeFail, "请求来源不允许")
			return
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == http.MethodOptions && c.Request.Header.Get("Access-Control-Request-Method") != "" {
			if !standardMethod(c.Request.Header.Get("Access-Control-Request-Method")) {
				writeFailure(c, http.StatusForbidden, CodeFail, "请求方法不允许")
				return
			}
			for _, header := range strings.Split(c.Request.Header.Get("Access-Control-Request-Headers"), ",") {
				switch strings.ToLower(strings.TrimSpace(header)) {
				case "", "content-type", "authorization", "x-requested-with", TokenCookie:
				default:
					writeFailure(c, http.StatusForbidden, CodeFail, "请求头不允许")
					return
				}
			}
			c.Header("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
			c.Header("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS, CONNECT, TRACE")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, "+TokenCookie)
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// 用途：只检查本进程与数据库就绪，返回非敏感健康状态。
func Health(db *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if db == nil || db.PingContext(ctx) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "{\"status\":\"unavailable\"}\n")
			return
		}
		_, _ = io.WriteString(w, "{\"status\":\"ok\"}\n")
	})
}

func standardMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return true
	}
	return false
}

func ensureVisitor(c *gin.Context, cfg Config) (string, error) {
	if cookie, err := c.Request.Cookie(VisitorCookie); err == nil {
		decoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
		if err == nil && len(decoded) == 32 {
			return cookie.Value, nil
		}
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("create visitor session: %w", err)
	}
	value := base64.RawURLEncoding.EncodeToString(random)
	http.SetCookie(c.Writer, &http.Cookie{
		Name: VisitorCookie, Value: value, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: c.Request.TLS != nil || strings.HasPrefix(cfg.PublicURL, "https://"),
	})
	// Replace malformed/duplicate visitor cookies before the identity resolver sees the request.
	cookies := c.Request.Cookies()
	c.Request.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name != VisitorCookie {
			c.Request.AddCookie(cookie)
		}
	}
	c.Request.AddCookie(&http.Cookie{Name: VisitorCookie, Value: value})
	return value, nil
}

func writeError(c *gin.Context, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeFailure(c, http.StatusRequestEntityTooLarge, CodeInvalid, "请求内容过大")
		return
	}
	var business *Error
	if errors.As(err, &business) && business != nil {
		switch business.Code {
		case CodeFail, CodeInvalid, CodeExists, CodeMissing, CodeQQ, CodeWeibo:
			writeFailure(c, http.StatusOK, business.Code, business.Message)
			return
		}
	}
	writeFailure(c, http.StatusOK, CodeSystem, "系统异常")
}

func writeFailure(c *gin.Context, status, code int, message string) {
	if !c.Writer.Written() {
		c.JSON(status, Result{Flag: false, Code: code, Message: message, Data: nil})
	}
	c.Abort()
}

func normalizeResult(value any) any {
	switch v := value.(type) {
	case Page:
		return Object{"recordList": normalizeResult(v.RecordList), "count": v.Count}
	case *Page:
		if v == nil {
			return nil
		}
		return normalizeResult(*v)
	case Object:
		if v == nil {
			return nil
		}
		object := make(Object, len(v))
		for key, item := range v {
			object[key] = normalizeResult(item)
		}
		return object
	case map[string]any:
		return normalizeResult(Object(v))
	}
	if value == nil {
		return nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Slice {
		// Raw JSON and byte buffers retain their normal JSON encodings.
		if _, ok := value.(json.RawMessage); ok {
			return value
		}
		if _, ok := value.([]byte); ok {
			return value
		}
		items := make([]any, reflected.Len())
		for index := range items {
			items[index] = normalizeResult(reflected.Index(index).Interface())
		}
		return items
	}
	return value
}

func auditIDs(in Input) []int64 {
	found := make(map[int64]bool)
	add := func(text string) {
		if id, err := strconv.ParseInt(text, 10, 64); err == nil && id > 0 {
			found[id] = true
		}
	}
	for key, value := range in.Params {
		if key == "id" || strings.HasSuffix(key, "Id") {
			add(value)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(in.Body))
	decoder.UseNumber()
	var body any
	if decoder.Decode(&body) == nil {
		addNumber := func(value any) {
			if number, ok := value.(json.Number); ok {
				add(number.String())
			}
		}
		switch body := body.(type) {
		case []any:
			for _, value := range body {
				addNumber(value)
			}
		case map[string]any:
			for _, key := range []string{"id", "userInfoId", "albumId"} {
				addNumber(body[key])
			}
			for _, key := range []string{"idList", "photoIdList"} {
				if ids, ok := body[key].([]any); ok {
					for _, value := range ids {
						addNumber(value)
					}
				}
			}
		}
	}
	ids := make([]int64, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func userAgent(value string) (browser, os string) {
	browser, os = "Unknown", "Unknown"
	switch {
	case strings.Contains(value, "Edg/") || strings.Contains(value, "Edge/"):
		browser = "Edge"
	case strings.Contains(value, "OPR/") || strings.Contains(value, "Opera"):
		browser = "Opera"
	case strings.Contains(value, "Chrome/") || strings.Contains(value, "CriOS/"):
		browser = "Chrome"
	case strings.Contains(value, "Firefox/") || strings.Contains(value, "FxiOS/"):
		browser = "Firefox"
	case strings.Contains(value, "Safari/"):
		browser = "Safari"
	case strings.Contains(value, "MSIE") || strings.Contains(value, "Trident/"):
		browser = "Internet Explorer"
	}
	switch {
	case strings.Contains(value, "Windows NT 10.0"):
		os = "Windows 10"
	case strings.Contains(value, "Windows NT 6.3"):
		os = "Windows 8.1"
	case strings.Contains(value, "Windows NT 6.1"):
		os = "Windows 7"
	case strings.Contains(value, "Windows"):
		os = "Windows"
	case strings.Contains(value, "Android"):
		os = "Android"
	case strings.Contains(value, "iPhone") || strings.Contains(value, "iPad"):
		os = "iOS"
	case strings.Contains(value, "Mac OS X"):
		os = "Mac OS X"
	case strings.Contains(value, "Linux"):
		os = "Linux"
	}
	return browser, os
}

// P-001 HTTP GET /healthz
// 响应变化：新增JSON {status:"ok"}；数据库不可用时503，不输出配置。
// 约束变化：新增本机进程/数据库就绪检查，JSON {status:"ok"}；数据库不可用503，不包含配置；只证明就绪，不代表业务验收。 具体字段与状态解释见MODULE-001已确认执行边界。
