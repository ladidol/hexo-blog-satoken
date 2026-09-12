package social

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"blog-go/internal/platform"
)

// 用途：拥有QQ 与微博身份核验的状态、数据访问及下列显式依赖。
type Service struct {
	db     *sql.DB
	cfg    platform.Config
	now    func() time.Time
	client *http.Client
}

// 用途：构造QQ 与微博身份核验，由 app 唯一装配依赖。
func New(db *sql.DB, cfg platform.Config, client *http.Client, now func() time.Time) *Service {
	owned := http.Client{}
	if client != nil {
		owned = *client
	}
	if owned.Timeout <= 0 || owned.Timeout > 5*time.Second {
		owned.Timeout = 5 * time.Second
	}
	owned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	owned.Jar = nil
	if owned.Transport == nil {
		owned.Transport = http.DefaultTransport
	}
	if transport, ok := owned.Transport.(*http.Transport); ok {
		// Keep the injected connection settings, but never inherit disabled TLS
		// verification or mutate a client shared with another adapter.
		transport = transport.Clone()
		if transport.TLSClientConfig != nil {
			transport.TLSClientConfig.InsecureSkipVerify = false
		}
		owned.Transport = transport
	}
	values := make(map[string]string, len(cfg.Values))
	for key, value := range cfg.Values {
		values[key] = value
	}
	cfg.Values = values
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, cfg: cfg, client: &owned, now: now}
}

// 用途：把经过提供方验证的身份和公开资料交给账号模块，不携带令牌。
type VerifiedIdentity struct {
	LoginType  int
	ExternalID string
	Nickname   string
	Avatar     string
}

// A provider failure exposes only the stable business message, while errors.As
// and errors.Is retain the business code and underlying I/O/cancellation cause.
type providerFailure struct {
	business *platform.Error
	cause    error
}

func (e *providerFailure) Error() string   { return e.business.Error() }
func (e *providerFailure) Unwrap() []error { return []error{e.business, e.cause} }

func loginFailure(code int, message string, cause error) error {
	business := &platform.Error{Code: code, Message: message}
	if cause == nil {
		return business
	}
	return &providerFailure{business: business, cause: cause}
}

// requestJSON owns and closes each response. The caller supplies one context
// deadline for the whole verification chain, including body reads.
func (s *Service) requestJSON(ctx context.Context, method, endpoint string, params url.Values, callback bool) (map[string]json.RawMessage, error) {
	var body io.Reader
	if method == http.MethodGet {
		endpoint += "?" + params.Encode()
	} else {
		body = strings.NewReader(params.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	const maxBody = 1 << 20
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.ContentLength > maxBody {
		return nil, errors.New("provider HTTP response rejected")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > maxBody {
		return nil, errors.New("provider response exceeds size limit")
	}
	payload := strings.TrimSpace(string(data))
	if callback && strings.HasPrefix(payload, "callback") {
		payload = strings.TrimSpace(strings.TrimPrefix(payload, "callback"))
		payload = strings.TrimSpace(strings.TrimSuffix(payload, ";"))
		if !strings.HasPrefix(payload, "(") || !strings.HasSuffix(payload, ")") {
			return nil, errors.New("invalid provider callback")
		}
		payload = payload[1 : len(payload)-1]
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &result); err != nil || result == nil {
		// JSON parser errors can include response content. Keep it out of the
		// error chain, since malformed provider data may contain credentials.
		return nil, errors.New("invalid provider JSON")
	}
	for _, field := range []string{"error", "error_code", "error_description", "request"} {
		if _, exists := result[field]; exists {
			return nil, errors.New("provider rejected authorization")
		}
	}
	return result, nil
}

func textField(object map[string]json.RawMessage, key string) string {
	var value string
	if json.Unmarshal(object[key], &value) != nil || strings.TrimSpace(value) == "" {
		return ""
	}
	return value
}

func avatarField(object map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if value := textField(object, key); value != "" {
			return value
		}
	}
	return ""
}
