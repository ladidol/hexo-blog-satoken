package analytics_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"blog-go/internal/analytics"
	"blog-go/internal/platform"
)

func TestLookupIPControlledProviderContractAndDisabled(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if r.Method != "GET" || q.Get("query") != "203.0.113.3" || q.Get("resource_id") != "6006" || q.Get("oe") != "utf8" || len(q["co"]) != 1 || q.Get("co") != "" {
			t.Error("provider contract", r.URL)
		}
		io.WriteString(w, `{"status":"0","data":[{"location":"四川省成都市"}]}`)
	}))
	defer server.Close()
	cfg := platform.Config{Values: map[string]string{"BLOG_IP_ENDPOINT": server.URL}}
	if got := analytics.New(nil, cfg, server.Client(), nil).LookupIP(background, "203.0.113.3"); got != "" || calls.Load() != 0 {
		t.Fatal("disabled lookup contacted provider", got)
	}
	cfg.Values["BLOG_IP_ENABLED"] = "true"
	if got := analytics.New(nil, cfg, server.Client(), nil).LookupIP(background, "203.0.113.3"); got != "四川省成都市" || calls.Load() != 1 {
		t.Fatal(got, calls.Load())
	}
}

func TestLookupIPDegradesForInvalidResponsesAndConfiguration(t *testing.T) {
	for name, body := range map[string]string{"empty": "", "malformed": "{", "empty-data": `{"data":[]}`, "wrong-shape": `{"data":{}}`, "empty-location": `{"data":[{"location":"  "}]}`, "wrong-location": `{"data":[{"location":1}]}`, "trailing": `{"data":[{"location":"四川"}]} garbage`, "provider-failure": `{"status":"1","data":[{"location":"四川"}]}`, "oversized": strings.Repeat(" ", 1<<20) + `{"data":[{"location":"四川"}]}`} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
			defer server.Close()
			s := analytics.New(nil, platform.Config{Values: map[string]string{"BLOG_IP_ENABLED": "true", "BLOG_IP_ENDPOINT": server.URL}}, server.Client(), nil)
			if got := s.LookupIP(background, "203.0.113.3"); got != "" {
				t.Fatal(got)
			}
		})
	}
	for _, endpoint := range []string{"", "file:///tmp/geo", "http://user:secret@localhost/geo", "::bad"} {
		s := analytics.New(nil, platform.Config{Values: map[string]string{"BLOG_IP_ENABLED": "true", "BLOG_IP_ENDPOINT": endpoint}}, nil, nil)
		if got := s.LookupIP(background, "203.0.113.3"); got != "" {
			t.Fatal(got)
		}
	}
	for _, status := range []int{http.StatusBadGateway, http.StatusFound} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "/redirect")
			w.WriteHeader(status)
			io.WriteString(w, `{"data":[{"location":"四川"}]}`)
		}))
		s := analytics.New(nil, platform.Config{Values: map[string]string{"BLOG_IP_ENABLED": "true", "BLOG_IP_ENDPOINT": server.URL}}, server.Client(), nil)
		if got := s.LookupIP(background, "203.0.113.3"); got != "" {
			t.Fatal(got)
		}
		server.Close()
	}
}

func TestLookupIPTimeoutCancellationAndReportAvailability(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); <-r.Context().Done() }))
	defer server.Close()
	db, cfg := setup(t)
	cfg.Values = map[string]string{"BLOG_IP_ENABLED": "true", "BLOG_IP_ENDPOINT": server.URL}
	supplied := server.Client()
	supplied.Timeout = 10 * time.Second
	s := analytics.New(db, cfg, supplied, nil)
	started := time.Now()
	if got := s.LookupIP(background, "203.0.113.3"); got != "" {
		t.Fatal(got)
	}
	if elapsed := time.Since(started); elapsed < 1800*time.Millisecond || elapsed > 3*time.Second {
		t.Fatal("lookup timeout not bounded to 2 seconds", elapsed)
	}
	if supplied.Timeout != 10*time.Second {
		t.Fatal("mutated caller HTTP client")
	}
	ctx, cancel := context.WithCancel(background)
	cancel()
	started = time.Now()
	if got := s.LookupIP(ctx, "203.0.113.3"); got != "" || time.Since(started) > time.Second {
		t.Fatal("ignored cancellation", got)
	}
	server.Close()
	if _, err := s.Report(background, platform.Input{IP: "203.0.113.3", Browser: "Chrome", OS: "Linux"}); err != nil {
		t.Fatal("geo failure blocked visits", err)
	}
	if scalar(t, db, "SELECT count(*) FROM tb_visit_record WHERE ip_source=''") != 1 {
		t.Fatal("fabricated region or missing visit")
	}
}
