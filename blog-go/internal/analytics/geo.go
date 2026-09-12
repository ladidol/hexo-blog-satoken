package analytics

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LookupIP is optional enrichment: errors never determine the calling business outcome.
func (s *Service) LookupIP(ctx context.Context, ip string) string {
	if s.cfg.Values["BLOG_IP_ENABLED"] != "true" {
		return ""
	}
	failed := func(reason string) string { log.Printf("IP region lookup unavailable: %s", reason); return "" }
	endpoint, err := url.Parse(s.cfg.Values["BLOG_IP_ENDPOINT"])
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return failed("configuration")
	}
	if net.ParseIP(ip) == nil {
		return failed("invalid address")
	}
	query := endpoint.Query()
	query.Set("query", ip)
	query.Set("co", "")
	query.Set("resource_id", "6006")
	query.Set("oe", "utf8")
	endpoint.RawQuery = query.Encode()
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(bounded, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return failed("request")
	}
	response, err := s.client.Do(request)
	if err != nil {
		return failed("network")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return failed("HTTP status")
	}
	const maxResponse = 64 << 10
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(body) > maxResponse {
		return failed("response size or read")
	}
	var payload struct {
		Status json.RawMessage `json:"status"`
		Data   []struct {
			Location string `json:"location"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return failed("response format")
	}
	if len(payload.Status) > 0 && string(payload.Status) != "0" && string(payload.Status) != `"0"` {
		return failed("provider status")
	}
	if len(payload.Data) == 0 {
		return failed("empty response")
	}
	location := strings.TrimSpace(payload.Data[0].Location)
	if location == "" || len(location) > 1024 {
		return failed("empty or invalid location")
	}
	return location
}

// P-105 HTTP HTTP 原百度IP来源查询
// Original query/co/resource_id/oe and data[0].location are preserved. Controlled
// provider tests do not establish live availability; deployers must verify it before enabling.
