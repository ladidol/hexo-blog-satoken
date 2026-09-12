package platform

import (
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// 用途：原公开协议中字段名到值的内部载体；只允许本评审列出的字段投影。
type Object map[string]any

// 用途：原两端共同消费的业务结果封装。
type Result struct {
	Flag    bool   `json:"flag"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// 用途：原两端管理/归档分页结果，空记录保持数组。
type Page struct {
	RecordList []Object `json:"recordList"`
	Count      int64    `json:"count"`
}

// 用途：区分登录账号与资料主键，归属由后端解析。
// Raw handlers read the resolved actor with r.Context().Value(Actor{}).
type Actor struct {
	AuthID     int64
	UserInfoID int64
	VisitorKey string
	Nickname   string
	Avatar     string
	LoggedIn   bool
}

// 用途：携带从原 query/form/JSON/multipart 解析的请求，保留写 cookie 的响应对象。
// Input.VisitorKey is the opaque browser cookie. Persistent viewer/guest-owner
// hashes are derived by their owning modules; the cookie grants no login rights.
type Input struct {
	Actor      Actor
	Params     map[string]string
	Query      url.Values
	Form       url.Values
	Body       json.RawMessage
	Files      []*multipart.FileHeader
	VisitorKey string
	IP         string
	Browser    string
	OS         string
	Request    *http.Request
	Response   http.ResponseWriter
}

// 用途：固定模块对 HTTP 的业务操作签名，返回真实值或明确业务错误。
// An endpoint may return Result{Flag:true, Code:CodeOK, ...} to preserve a
// business-specific success message. Failures use error rather than a Result.
type Endpoint func(context.Context, Input) (any, error)

// 用途：供路由匹配、授权与审计使用同一注册元数据。
type RouteSpec struct {
	Method    string
	Path      string
	Operation string
	Policy    string
	Audit     bool
}

// 用途：把注册协议声明绑定到对应模块实现，不自动放行未知路径。
type Route struct {
	Spec   RouteSpec
	Handle Endpoint
	Raw    http.Handler
}

// 用途：承载稳定错误码，禁止输出底层凭据或完整 provider 请求。
type Error struct {
	Code    int
	Message string
}

// 用途：实现 error 供统一 HTTP 包装处理。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// 用途：保留两端已使用的业务结果编码。
const (
	CodeOK      = 20000
	CodeSystem  = 50000
	CodeFail    = 51000
	CodeInvalid = 52000
	CodeExists  = 52001
	CodeMissing = 52002
	CodeQQ      = 53001
	CodeWeibo   = 53002
)

// 用途：固定登录 cookie 与非鉴权浏览会话 cookie 的名称。
const (
	TokenCookie   = "xiaoxiao-token"
	VisitorCookie = "xiaoxiao-visitor"
)

// 用途：只给审计记录传递白名单动作与结果摘要。
type AuditEvent struct {
	Actor     Actor
	Route     RouteSpec
	ObjectIDs []int64
	Count     int
	Code      int
	IP        string
	At        time.Time
}

// Public protocol projections use platform.Object; no database row is serialized directly.
// Integer/Long: JSON number; Date: Unix milliseconds; LocalDateTime: yyyy-MM-ddTHH:mm:ss.
// Optional fields retain null; lists/sets are arrays. UserDetailDTO never includes password.
// ConditionVO: current:Long, size:Long, keywords:String, categoryId:Integer, tagId:Integer, albumId:Integer, loginType:Integer, type:Integer, status:Integer, startTime:LocalDateTime, endTime:LocalDateTime, isDelete:Integer, isReview:Integer
// DeleteVO: idList:Integer[], isDelete:Integer
// ReviewVO: idList:Integer[], isReview:Integer
// PageResult: recordList:T[], count:Integer
