package social_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"blog-go/internal/platform"
	"blog-go/internal/social"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func configuration() platform.Config {
	return platform.Config{Values: map[string]string{
		"BLOG_QQ_APP_ID": "10001", "BLOG_WEIBO_APP_KEY": "20002",
		"BLOG_WEIBO_APP_SECRET": "secret&+", "BLOG_WEIBO_REDIRECT_URI": "https://blog.example/oauth?app=weibo",
	}}
}

type responseBody struct {
	io.Reader
	closed bool
}

func (b *responseBody) Close() error { b.closed = true; return nil }

func providerClient(t *testing.T, bodies []string, check func(int, *http.Request)) (*http.Client, *int) {
	t.Helper()
	calls := 0
	var opened []*responseBody
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		index := calls
		calls++
		if index >= len(bodies) {
			t.Errorf("unexpected provider request: %s", r.URL.Path)
			return nil, errors.New("unexpected provider request")
		}
		if r.URL.Scheme != "https" {
			t.Error("provider request must use HTTPS")
		}
		if check != nil {
			check(index, r)
		}
		body := &responseBody{Reader: strings.NewReader(bodies[index])}
		opened = append(opened, body)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body, Request: r}, nil
	})}
	t.Cleanup(func() {
		for _, body := range opened {
			if !body.closed {
				t.Error("provider response body was not closed")
			}
		}
	})
	return client, &calls
}

func failed(t *testing.T, got social.VerifiedIdentity, err error, code int) {
	t.Helper()
	var business *platform.Error
	if err == nil || !errors.As(err, &business) || business.Code != code {
		t.Fatalf("error = %v, want provider code %d", err, code)
	}
	if got != (social.VerifiedIdentity{}) {
		t.Fatalf("failure leaked partial identity: %+v", got)
	}
	for _, credential := range []string{"token&+", "secret&+", "code&+", "raw-provider-secret", "access_token=", "client_secret="} {
		if strings.Contains(err.Error(), credential) {
			t.Errorf("error exposes credentials: %s", err)
		}
	}
}

func TestVerifyQQ(t *testing.T) {
	for _, tc := range []struct{ name, me, profile, avatar string }{
		{"json", `{"client_id":"10001","openid":"open-id"}`, `{"ret":0,"nickname":"昵称","figureurl_qq_2":"https://img.example/hd","figureurl_qq_1":"https://img.example/low"}`, "https://img.example/hd"},
		{"callback and avatar fallback", ` callback( {"client_id":"10001","openid":"open-id"} ); `, `{"ret":0,"nickname":"昵称","figureurl_qq_1":"https://img.example/low"}`, "https://img.example/low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, calls := providerClient(t, []string{tc.me, tc.profile}, func(index int, r *http.Request) {
				want := url.Values{"access_token": {"token&+"}, "fmt": {"json"}}
				path := "/oauth2.0/me"
				if index == 1 {
					path = "/user/get_user_info"
					want = url.Values{"access_token": {"token&+"}, "oauth_consumer_key": {"10001"}, "openid": {"open-id"}}
				}
				if r.Method != "GET" || r.URL.Host != "graph.qq.com" || r.URL.Path != path || !reflect.DeepEqual(r.URL.Query(), want) {
					t.Errorf("incorrect QQ request method/host/path/parameters at step %d", index)
				}
			})
			service := social.New(nil, configuration(), client, nil)
			got, err := service.VerifyQQ(context.Background(), "open-id", "token&+")
			want := social.VerifiedIdentity{LoginType: 2, ExternalID: "open-id", Nickname: "昵称", Avatar: tc.avatar}
			if err != nil || got != want || *calls != 2 {
				t.Fatalf("QQ = %+v, %v, calls %d; want %+v", got, err, *calls, want)
			}
		})
	}
}

func TestVerifyQQRejectsInvalidIdentityAndProfile(t *testing.T) {
	me := `{"client_id":"10001","openid":"open-id"}`
	profile := `{"ret":0,"nickname":"name","figureurl_qq_2":"avatar"}`
	for _, tc := range []struct {
		name, me, profile string
		calls             int
	}{
		{"other application", `{"client_id":"other","openid":"open-id"}`, profile, 1},
		{"other identity", `{"client_id":"10001","openid":"other"}`, profile, 1},
		{"missing application", `{"openid":"open-id"}`, profile, 1},
		{"provider error", `callback({"error":100016,"error_description":"raw-provider-secret"});`, profile, 1},
		{"description without code", `{"client_id":"10001","openid":"open-id","error_description":"raw-provider-secret"}`, profile, 1},
		{"malformed callback", `callback({"client_id":"10001","openid":"open-id"});alert(1)`, profile, 1},
		{"unknown callback", `evil({"client_id":"10001","openid":"open-id"});`, profile, 1},
		{"trailing JSON", me + `{}`, profile, 1},
		{"null identity", `null`, profile, 1},
		{"profile rejection", me, `{"ret":1002,"nickname":"name","figureurl_qq_2":"avatar"}`, 2},
		{"missing ret", me, `{"nickname":"name","figureurl_qq_2":"avatar"}`, 2},
		{"empty nickname", me, `{"ret":0,"nickname":" ","figureurl_qq_2":"avatar"}`, 2},
		{"empty avatar", me, `{"ret":0,"nickname":"name"}`, 2},
		{"wrong ret type", me, `{"ret":"0","nickname":"name","figureurl_qq_2":"avatar"}`, 2},
		{"profile business error", me, `{"ret":0,"nickname":"name","figureurl_qq_2":"avatar","error":1001}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, calls := providerClient(t, []string{tc.me, tc.profile}, nil)
			got, err := social.New(nil, configuration(), client, nil).VerifyQQ(context.Background(), "open-id", "token&+")
			failed(t, got, err, platform.CodeQQ)
			if *calls != tc.calls {
				t.Fatalf("calls = %d, want %d", *calls, tc.calls)
			}
		})
	}
}

func TestVerifyWeibo(t *testing.T) {
	for _, avatar := range []string{"avatar_hd", "avatar_large", "profile_image_url"} {
		t.Run(avatar, func(t *testing.T) {
			const uid = "900719925474099312345678"
			client, calls := providerClient(t, []string{
				`{"access_token":"token&+","uid":` + uid + `}`,
				`{"appkey":20002,"uid":"` + uid + `","expire_in":"3600"}`,
				`{"id":` + uid + `,"idstr":"` + uid + `","screen_name":"微博用户","` + avatar + `":"https://img.example/avatar"}`,
			}, func(index int, r *http.Request) {
				paths := []string{"/oauth2/access_token", "/oauth2/get_token_info", "/2/users/show.json"}
				if r.URL.Host != "api.weibo.com" || r.URL.Path != paths[index] {
					t.Errorf("incorrect Weibo endpoint at step %d", index)
				}
				if index == 2 {
					if r.Method != "GET" || !reflect.DeepEqual(r.URL.Query(), url.Values{"access_token": {"token&+"}, "uid": {uid}}) {
						t.Error("incorrect profile request")
					}
					return
				}
				if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
					t.Error("token requests must use POST form")
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				want := url.Values{"access_token": {"token&+"}}
				if index == 0 {
					want = url.Values{"client_id": {"20002"}, "client_secret": {"secret&+"}, "grant_type": {"authorization_code"}, "code": {"code&+"}, "redirect_uri": {"https://blog.example/oauth?app=weibo"}}
				}
				if !reflect.DeepEqual(r.PostForm, want) {
					t.Errorf("incorrect Weibo form at step %d", index)
				}
			})
			got, err := social.New(nil, configuration(), client, nil).VerifyWeibo(context.Background(), "code&+")
			want := social.VerifiedIdentity{LoginType: 3, ExternalID: uid, Nickname: "微博用户", Avatar: "https://img.example/avatar"}
			if err != nil || got != want || *calls != 3 {
				t.Fatalf("Weibo = %+v, %v, calls %d; want %+v", got, err, *calls, want)
			}
		})
	}
}

func TestVerifyWeiboRejectsInvalidAuthorization(t *testing.T) {
	valid := []string{`{"access_token":"token&+","uid":"123"}`, `{"appkey":"20002","uid":123,"expire_in":3600}`, `{"idstr":"123","screen_name":"name","avatar_hd":"avatar"}`}
	for _, tc := range []struct {
		name string
		step int
		body string
	}{
		{"exchange rejected", 0, `{"error":"raw-provider-secret","error_code":21325}`},
		{"missing token", 0, `{"uid":"123"}`},
		{"empty token", 0, `{"access_token":" ","uid":"123"}`},
		{"missing uid", 0, `{"access_token":"token&+"}`},
		{"fractional uid", 0, `{"access_token":"token&+","uid":123.5}`},
		{"other application", 1, `{"appkey":"1","uid":123,"expire_in":3600}`},
		{"other token identity", 1, `{"appkey":"20002","uid":124,"expire_in":3600}`},
		{"expired", 1, `{"appkey":"20002","uid":123,"expire_in":0}`},
		{"negative expiry", 1, `{"appkey":"20002","uid":123,"expire_in":-1}`},
		{"missing expiry", 1, `{"appkey":"20002","uid":123}`},
		{"expired provider code", 1, `{"error":"expired","error_code":21327}`},
		{"rate limited", 1, `{"error_code":10023}`},
		{"provider request error", 1, `{"appkey":"20002","uid":123,"expire_in":3600,"request":"raw-provider-secret"}`},
		{"other profile", 2, `{"idstr":"124","screen_name":"name","avatar_hd":"avatar"}`},
		{"conflicting profile IDs", 2, `{"id":124,"idstr":"123","screen_name":"name","avatar_hd":"avatar"}`},
		{"null profile ID", 2, `{"id":null,"idstr":"123","screen_name":"name","avatar_hd":"avatar"}`},
		{"missing profile ID", 2, `{"screen_name":"name","avatar_hd":"avatar"}`},
		{"empty nickname", 2, `{"id":123,"screen_name":" ","avatar_hd":"avatar"}`},
		{"empty avatar", 2, `{"id":123,"screen_name":"name"}`},
		{"profile rejection", 2, `{"id":123,"screen_name":"name","avatar_hd":"avatar","error_code":21315}`},
		{"malformed profile", 2, `<html>raw-provider-secret</html>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies := append([]string(nil), valid...)
			bodies[tc.step] = tc.body
			client, calls := providerClient(t, bodies, nil)
			got, err := social.New(nil, configuration(), client, nil).VerifyWeibo(context.Background(), "code&+")
			failed(t, got, err, platform.CodeWeibo)
			if *calls != tc.step+1 {
				t.Fatalf("calls = %d, want %d", *calls, tc.step+1)
			}
		})
	}
}

func TestSocialMissingConfigurationAndInputs(t *testing.T) {
	for _, key := range []string{"BLOG_QQ_APP_ID", "BLOG_WEIBO_APP_KEY", "BLOG_WEIBO_APP_SECRET", "BLOG_WEIBO_REDIRECT_URI", "qq-openid", "qq-token", "weibo-code"} {
		t.Run(key, func(t *testing.T) {
			cfg := configuration()
			openID, token, code := "open-id", "token&+", "code&+"
			switch key {
			case "qq-openid":
				openID = " "
			case "qq-token":
				token = " "
			case "weibo-code":
				code = " "
			default:
				cfg.Values[key] = " "
			}
			client, calls := providerClient(t, nil, nil)
			service := social.New(nil, cfg, client, nil)
			if strings.Contains(key, "QQ") || strings.HasPrefix(key, "qq-") {
				got, err := service.VerifyQQ(context.Background(), openID, token)
				failed(t, got, err, platform.CodeQQ)
			} else {
				got, err := service.VerifyWeibo(context.Background(), code)
				failed(t, got, err, platform.CodeWeibo)
			}
			if *calls != 0 {
				t.Fatal("invalid configuration/input reached provider")
			}
		})
	}
}

func TestSocialHTTPFailures(t *testing.T) {
	for _, provider := range []string{"qq", "weibo"} {
		for _, kind := range []string{"redirect", "non-success", "oversized", "transport", "read"} {
			t.Run(provider+"/"+kind, func(t *testing.T) {
				calls := 0
				closed := false
				cause := errors.New("raw-provider-secret")
				client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if calls > 1 {
						t.Error("failure followed by provider request")
						return nil, cause
					}
					if kind == "transport" {
						return nil, cause
					}
					status := 200
					header := make(http.Header)
					if kind == "redirect" {
						status = 302
						header.Set("Location", "https://other.example/steal")
					}
					if kind == "non-success" {
						status = 429
					}
					var reader io.Reader = strings.NewReader(strings.Repeat(" ", 1<<20) + "{}")
					if kind == "read" {
						reader = errorReader{cause}
					}
					return &http.Response{StatusCode: status, Header: header, Body: &closingReader{Reader: reader, closed: &closed}, Request: r}, nil
				})}
				service := social.New(nil, configuration(), client, nil)
				var got social.VerifiedIdentity
				var err error
				code := platform.CodeQQ
				if provider == "qq" {
					got, err = service.VerifyQQ(context.Background(), "open-id", "token&+")
				} else {
					code = platform.CodeWeibo
					got, err = service.VerifyWeibo(context.Background(), "code&+")
				}
				failed(t, got, err, code)
				if calls != 1 || kind != "transport" && !closed {
					t.Errorf("calls=%d, body closed=%v", calls, closed)
				}
				if (kind == "transport" || kind == "read") && !errors.Is(err, cause) {
					t.Error("lost underlying error identity")
				}
			})
		}
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type closingReader struct {
	io.Reader
	closed *bool
}

func (r *closingReader) Close() error { *r.closed = true; return nil }

func TestSocialCancellationAndTotalDeadline(t *testing.T) {
	for _, provider := range []string{"qq", "weibo"} {
		t.Run(provider, func(t *testing.T) {
			var firstDeadline time.Time
			bodies := []string{`{"client_id":"10001","openid":"open-id"}`, `{"ret":0,"nickname":"name","figureurl_qq_2":"avatar"}`}
			if provider == "weibo" {
				bodies = []string{`{"access_token":"token&+","uid":"123"}`, `{"appkey":20002,"uid":"123","expire_in":3600}`, `{"id":"123","screen_name":"name","avatar_hd":"avatar"}`}
			}
			client, calls := providerClient(t, bodies, func(index int, r *http.Request) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("missing five-second total deadline")
				}
				if index == 0 {
					firstDeadline = deadline
					time.Sleep(5 * time.Millisecond)
				} else if !deadline.Equal(firstDeadline) {
					t.Error("deadline reset for later request")
				}
			})
			service := social.New(nil, configuration(), client, nil)
			var err error
			if provider == "qq" {
				_, err = service.VerifyQQ(context.Background(), "open-id", "token&+")
			} else {
				_, err = service.VerifyWeibo(context.Background(), "code&+")
			}
			if err != nil || *calls != len(bodies) {
				t.Fatalf("verification = %v, calls=%d", err, *calls)
			}
			client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
			service = social.New(nil, configuration(), client, nil)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var got social.VerifiedIdentity
			code := platform.CodeQQ
			if provider == "qq" {
				got, err = service.VerifyQQ(ctx, "open-id", "token&+")
			} else {
				code = platform.CodeWeibo
				got, err = service.VerifyWeibo(ctx, "code&+")
			}
			failed(t, got, err, code)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("lost cancellation: %v", err)
			}
			ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if provider == "qq" {
				got, err = service.VerifyQQ(ctx, "open-id", "token&+")
			} else {
				got, err = service.VerifyWeibo(ctx, "code&+")
			}
			failed(t, got, err, code)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("lost caller deadline: %v", err)
			}
		})
	}
}

func TestSocialResponseSizeBoundary(t *testing.T) {
	me := `{"client_id":"10001","openid":"open-id"}`
	for _, tc := range []struct {
		name string
		size int
	}{{"maximum", 1 << 20}, {"oversized", 1<<20 + 1}} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := providerClient(t, []string{me + strings.Repeat(" ", tc.size-len(me)), `{"ret":0,"nickname":"name","figureurl_qq_2":"avatar"}`}, nil)
			got, err := social.New(nil, configuration(), client, nil).VerifyQQ(context.Background(), "open-id", "token&+")
			if tc.size == 1<<20 {
				if err != nil || got.ExternalID != "open-id" {
					t.Fatalf("maximum-size response failed: %+v, %v", got, err)
				}
			} else {
				failed(t, got, err, platform.CodeQQ)
			}
		})
	}
}

func TestSocialTLSVerification(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted TLS request reached provider") }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	got, err := social.New(nil, configuration(), client, nil).VerifyQQ(context.Background(), "open-id", "token&+")
	failed(t, got, err, platform.CodeQQ)
	if !transport.TLSClientConfig.InsecureSkipVerify || client.Timeout != 0 || client.CheckRedirect != nil {
		t.Error("constructor mutated caller's client")
	}
}

func TestSocialConcurrentCalls(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"client_id":"10001","openid":"open-id"}`
		switch r.URL.Path {
		case "/user/get_user_info":
			body = `{"ret":0,"nickname":"name","figureurl_qq_2":"avatar"}`
		case "/oauth2/access_token":
			body = `{"access_token":"token&+","uid":"123"}`
		case "/oauth2/get_token_info":
			body = `{"appkey":20002,"uid":"123","expire_in":3600}`
		case "/2/users/show.json":
			body = `{"id":123,"screen_name":"name","avatar_hd":"avatar"}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	service := social.New(nil, configuration(), client, nil)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := service.VerifyQQ(context.Background(), "open-id", "token&+")
			if err != nil || got.ExternalID != "open-id" {
				t.Errorf("concurrent QQ verification: %+v, %v", got, err)
			}
			got, err = service.VerifyWeibo(context.Background(), "code&+")
			if err != nil || got.ExternalID != "123" {
				t.Errorf("concurrent Weibo verification: %+v, %v", got, err)
			}
		}()
	}
	wg.Wait()
}
