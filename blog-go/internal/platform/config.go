package platform

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// 用途：持有单进程本地运行与外部适配环境值，不序列化到业务结果。
type Config struct {
	Addr           string
	DataDir        string
	PublicURL      string
	FrontendURL    string
	AllowedOrigins []string
	TimeZone       *time.Location
	Values         map[string]string
}

// 用途：读取/验证下方冻结的环境键，未配置外部能力保持真实失败或降级。
func LoadConfig() (Config, error) {
	defaults := map[string]string{
		"BLOG_ADDR": "127.0.0.1:8888", "BLOG_DATA_DIR": "./data",
		"BLOG_PUBLIC_URL": "http://localhost:8888", "BLOG_FRONTEND_URL": "http://localhost:8080",
		"BLOG_ALLOWED_ORIGINS": "http://localhost:8080,http://localhost:8081",
		"BLOG_TIMEZONE":        "Asia/Shanghai", "BLOG_UPLOAD_MODE": "local", "BLOG_IP_ENABLED": "false",
	}
	keys := []string{
		"BLOG_ADDR", "BLOG_DATA_DIR", "BLOG_PUBLIC_URL", "BLOG_FRONTEND_URL", "BLOG_ALLOWED_ORIGINS",
		"BLOG_TIMEZONE", "BLOG_UPLOAD_MODE", "BLOG_OSS_ENDPOINT", "BLOG_OSS_REGION", "BLOG_OSS_BUCKET",
		"BLOG_OSS_PUBLIC_URL", "BLOG_OSS_ACCESS_KEY_ID", "BLOG_OSS_ACCESS_KEY_SECRET",
		"BLOG_SMTP_HOST", "BLOG_SMTP_PORT", "BLOG_SMTP_TLS_MODE", "BLOG_SMTP_USERNAME",
		"BLOG_SMTP_PASSWORD", "BLOG_SMTP_FROM", "BLOG_QQ_APP_ID", "BLOG_WEIBO_APP_KEY",
		"BLOG_WEIBO_APP_SECRET", "BLOG_WEIBO_REDIRECT_URI", "BLOG_IP_ENABLED", "BLOG_IP_ENDPOINT",
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		values[key] = os.Getenv(key)
		if values[key] == "" {
			values[key] = defaults[key]
		}
	}
	// Diagnostics identify only the key, never its potentially sensitive value.
	invalid := func(key string) (Config, error) { return Config{}, fmt.Errorf("invalid %s", key) }
	host, port, err := net.SplitHostPort(values["BLOG_ADDR"])
	number, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || number < 0 || number > 65535 || strings.ContainsAny(host, " \r\n\t") {
		return invalid("BLOG_ADDR")
	}
	for _, key := range []string{"BLOG_PUBLIC_URL", "BLOG_FRONTEND_URL"} {
		if !validHTTPURL(values[key], false) {
			return invalid(key)
		}
	}
	var origins []string
	seen := make(map[string]bool)
	for _, origin := range strings.Split(values["BLOG_ALLOWED_ORIGINS"], ",") {
		origin = strings.TrimSuffix(strings.TrimSpace(origin), "/")
		if !validHTTPURL(origin, true) {
			return invalid("BLOG_ALLOWED_ORIGINS")
		}
		if !seen[origin] {
			origins = append(origins, origin)
			seen[origin] = true
		}
	}
	location, err := time.LoadLocation(values["BLOG_TIMEZONE"])
	if err != nil {
		return invalid("BLOG_TIMEZONE")
	}
	if mode := values["BLOG_UPLOAD_MODE"]; mode != "local" && mode != "oss" {
		return invalid("BLOG_UPLOAD_MODE")
	}
	if enabled := values["BLOG_IP_ENABLED"]; enabled != "true" && enabled != "false" {
		return invalid("BLOG_IP_ENABLED")
	}
	if port := values["BLOG_SMTP_PORT"]; port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return invalid("BLOG_SMTP_PORT")
		}
	}
	if mode := values["BLOG_SMTP_TLS_MODE"]; mode != "" && mode != "tls" && mode != "starttls" {
		return invalid("BLOG_SMTP_TLS_MODE")
	}
	if strings.TrimSpace(values["BLOG_DATA_DIR"]) == "" || strings.ContainsRune(values["BLOG_DATA_DIR"], 0) {
		return invalid("BLOG_DATA_DIR")
	}
	return Config{
		Addr: values["BLOG_ADDR"], DataDir: values["BLOG_DATA_DIR"],
		PublicURL:      strings.TrimRight(values["BLOG_PUBLIC_URL"], "/"),
		FrontendURL:    strings.TrimRight(values["BLOG_FRONTEND_URL"], "/"),
		AllowedOrigins: origins, TimeZone: location, Values: values,
	}, nil
}

func validHTTPURL(value string, originOnly bool) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Hostname() != "" && parsed.User == nil && parsed.RawQuery == "" &&
		parsed.Fragment == "" && !strings.ContainsAny(value, "\r\n\t") &&
		(!originOnly || parsed.Path == "")
}
