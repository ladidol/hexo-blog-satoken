package files_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"testing"

	"blog-go/internal/files"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type objectTransport struct {
	mu                                  sync.Mutex
	data                                []byte
	contentType, disposition            string
	calls                               []string
	headStatus, putStatus, deleteStatus int
	conflict                            bool
	fail                                error
	auth                                string
}

func (t *objectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	t.calls = append(t.calls, r.Method)
	t.auth = r.Header.Get("Authorization")
	if t.fail != nil {
		return nil, t.fail
	}
	status := 200
	var data []byte
	header := make(http.Header)
	switch r.Method {
	case "HEAD":
		status = t.headStatus
		if status == 0 {
			if t.data == nil {
				status = 404
			} else {
				status = 200
			}
		}
	case "PUT":
		status = t.putStatus
		if status == 0 {
			status = 200
		}
		if r.Header.Get("x-oss-forbid-overwrite") != "true" {
			return nil, errors.New("upload allowed overwrite")
		}
		if t.conflict {
			status = 409
			t.data, _ = io.ReadAll(r.Body)
		}
		if status == 200 {
			var err error
			t.data, err = io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			t.contentType = r.Header.Get("Content-Type")
			t.disposition = r.Header.Get("Content-Disposition")
		}
	case "DELETE":
		status = t.deleteStatus
		if status == 0 {
			status = 204
		}
		if status == 204 {
			t.data = nil
		}
	case "GET":
		if t.data == nil {
			status = 404
		} else {
			data = append([]byte(nil), t.data...)
			header.Set("Content-Type", t.contentType)
			header.Set("Content-Disposition", t.disposition)
		}
	}
	if status >= 400 {
		code := "AccessDenied"
		if status == 404 {
			code = "NoSuchKey"
		}
		if status == 409 {
			code = "FileAlreadyExists"
		}
		header.Set("x-oss-error-code", code)
		data = []byte("<Error><Code>" + code + "</Code><Message>test provider failure</Message></Error>")
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: header, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Request: r}, nil
}
func ossStore(transport *objectTransport) *files.OSSStore {
	client := oss.NewClient(oss.LoadDefaultConfig().WithRegion("cn-hangzhou").WithEndpoint("https://oss.example").WithSignatureVersion(oss.SignatureVersionV4).WithRetryMaxAttempts(1).WithHttpClient(&http.Client{Transport: transport}).WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test-id", "test-secret")))
	return &files.OSSStore{Client: client, Bucket: "test-bucket", PublicURL: "https://cdn.example/media"}
}
func TestOSSV4UploadDedupDownloadAndRemoval(t *testing.T) {
	transport := &objectTransport{}
	s := ossStore(transport)
	data := pngData(t)
	f, err := s.Put(context.Background(), "articles", "a.png", "image/png", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !f.Created || !bytes.Equal(transport.data, data) || transport.contentType != "image/png" || !strings.HasPrefix(transport.auth, "OSS4-HMAC-SHA256 ") || !strings.HasPrefix(f.URL, "https://cdn.example/media/articles/") {
		t.Fatalf("upload contract: %+v %+v", f, transport)
	}
	response, err := (&http.Client{Transport: transport}).Get(f.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	downloaded, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || !bytes.Equal(downloaded, data) {
		t.Fatal("browser URL unreadable")
	}
	reused, err := s.Put(context.Background(), "articles", "b.png", "image/png", bytes.NewReader(data))
	if err != nil || reused.Created || reused.Key != f.Key {
		t.Fatalf("dedup: %+v %v", reused, err)
	}
	count := len(transport.calls)
	if err := s.Remove(context.Background(), reused); err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) != count {
		t.Fatal("reused object deleted")
	}
	if err := s.Remove(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if transport.data != nil {
		t.Fatal("new object not removed")
	}
	export, err := s.Put(context.Background(), "exports", "中文文章.md", "text/markdown", strings.NewReader("# article"))
	if err != nil || !export.Created {
		t.Fatalf("export: %+v %v", export, err)
	}
	disposition, params, err := mime.ParseMediaType(transport.disposition)
	if err != nil || disposition != "attachment" || params["filename"] != "中文文章.md" || transport.contentType != "text/markdown; charset=utf-8" {
		t.Fatalf("download headers: %s %s", transport.disposition, transport.contentType)
	}
}
func TestOSSFailureNeverBecomesSuccess(t *testing.T) {
	data := pngData(t)
	for _, status := range []int{403, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			transport := &objectTransport{headStatus: status}
			s := ossStore(transport)
			f, err := s.Put(context.Background(), "articles", "a.png", "image/png", bytes.NewReader(data))
			var serviceErr *oss.ServiceError
			if err == nil || f.URL != "" || !errors.As(err, &serviceErr) || serviceErr.StatusCode != status || len(transport.calls) != 1 {
				t.Fatalf("HEAD error lost: %+v %v %v", f, err, transport.calls)
			}
		})
	}
	transport := &objectTransport{putStatus: 403}
	s := ossStore(transport)
	f, err := s.Put(context.Background(), "articles", "a.png", "image/png", bytes.NewReader(data))
	if err == nil || f.URL != "" || transport.data != nil {
		t.Fatalf("PUT failure hidden: %+v %v", f, err)
	}
	timeout := context.DeadlineExceeded
	transport = &objectTransport{fail: timeout}
	s = ossStore(transport)
	if _, err := s.Put(context.Background(), "articles", "a.png", "image/png", bytes.NewReader(data)); !errors.Is(err, timeout) {
		t.Fatalf("network error identity: %v", err)
	}
	transport = &objectTransport{deleteStatus: 403}
	s = ossStore(transport)
	file, err := s.Put(context.Background(), "articles", "a.png", "image/png", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(context.Background(), file); err == nil || transport.data == nil {
		t.Fatal("cleanup failure hidden")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Put(ctx, "articles", "a.png", "image/png", bytes.NewReader(data)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
}
func TestOnlyExplicitOSSModeConstructsClientAndExportsStayProtected(t *testing.T) {
	cfg := config(t)
	cfg.Values = map[string]string{"BLOG_OSS_ENDPOINT": "https://oss.example", "BLOG_OSS_REGION": "cn-hangzhou", "BLOG_OSS_BUCKET": "test-bucket", "BLOG_OSS_PUBLIC_URL": "https://cdn.example", "BLOG_OSS_ACCESS_KEY_ID": "test-id", "BLOG_OSS_ACCESS_KEY_SECRET": "test-secret"}
	transport := &objectTransport{}
	client := &http.Client{Transport: transport}
	data := pngData(t)
	local := files.New(nil, cfg, client, nil)
	if _, err := local.Put(context.Background(), "avatar", "a.png", "image/png", bytes.NewReader(data)); err != nil || len(transport.calls) != 0 {
		t.Fatal("implicit OSS enabled", err)
	}
	cfg.Values["BLOG_UPLOAD_MODE"] = "oss"
	remote := files.New(nil, cfg, client, nil)
	f, err := remote.Put(context.Background(), "avatar", "a.png", "image/png", bytes.NewReader(data))
	if err != nil || !strings.HasPrefix(f.URL, "https://cdn.example/") || len(transport.calls) != 2 {
		t.Fatalf("explicit OSS missing: %+v %v %v", f, err, transport.calls)
	}
	count := len(transport.calls)
	f, err = remote.Put(context.Background(), "exports", "private.md", "text/markdown", strings.NewReader("private"))
	if err != nil || !strings.HasPrefix(f.URL, cfg.PublicURL+"/files/exports/") || len(transport.calls) != count {
		t.Fatalf("private export bypassed local gate: %+v %v", f, err)
	}
	cfg.Values["BLOG_OSS_ENDPOINT"] = "http://oss.example"
	bad := files.New(nil, cfg, client, nil)
	if f, err := bad.Put(context.Background(), "avatar", "a.png", "image/png", bytes.NewReader(data)); err == nil || f.URL != "" {
		t.Fatal("insecure OSS config accepted")
	}
}

func TestOSSConcurrentWinnerIsReused(t *testing.T) {
	transport := &objectTransport{conflict: true}
	s := ossStore(transport)
	data := pngData(t)
	f, err := s.Put(context.Background(), "articles", "a.png", "image/png", bytes.NewReader(data))
	if err != nil || f.Created || f.URL == "" || !bytes.Equal(transport.data, data) {
		t.Fatalf("concurrent winner lost: %+v %v", f, err)
	}
	count := len(transport.calls)
	if err := s.Remove(context.Background(), f); err != nil || len(transport.calls) != count {
		t.Fatal("concurrent winner was deleted", err)
	}
}
