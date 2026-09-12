package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"blog-go/internal/platform"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type OSSStore struct {
	Client    *oss.Client
	Bucket    string
	PublicURL string
}

func newOSSStore(cfg platform.Config, client *http.Client) *OSSStore {
	values := cfg.Values
	result := &OSSStore{Bucket: values["BLOG_OSS_BUCKET"], PublicURL: strings.TrimRight(values["BLOG_OSS_PUBLIC_URL"], "/")}
	endpoint := values["BLOG_OSS_ENDPOINT"]
	if endpoint != "" && !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	public, publicErr := url.Parse(result.PublicURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || publicErr != nil || public.Scheme != "https" || public.Hostname() == "" || public.User != nil || public.RawQuery != "" || public.Fragment != "" || result.Bucket == "" || values["BLOG_OSS_REGION"] == "" || values["BLOG_OSS_ACCESS_KEY_ID"] == "" || values["BLOG_OSS_ACCESS_KEY_SECRET"] == "" {
		return result
	}
	config := oss.LoadDefaultConfig().WithRegion(values["BLOG_OSS_REGION"]).WithEndpoint(endpoint).
		WithSignatureVersion(oss.SignatureVersionV4).WithRetryMaxAttempts(3).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(values["BLOG_OSS_ACCESS_KEY_ID"], values["BLOG_OSS_ACCESS_KEY_SECRET"]))
	if client != nil {
		copyClient := *client
		if copyClient.Timeout == 0 {
			copyClient.Timeout = 30 * time.Second
		}
		copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		// Caller-provided transports remain useful for tests; production retains TLS validation.
		if transport, ok := copyClient.Transport.(*http.Transport); ok && transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
			return result
		}
		config.WithHttpClient(&copyClient)
	}
	result.Client = oss.NewClient(config)
	return result
}
func (s *OSSStore) Put(ctx context.Context, scope, name, contentType string, body io.Reader) (StoredFile, error) {
	input, err := prepareFile(ctx, scope, name, contentType, body)
	if err != nil {
		return StoredFile{}, err
	}
	if s.Client == nil || s.Bucket == "" || s.PublicURL == "" {
		return StoredFile{}, errors.New("OSS storage configuration is incomplete or insecure")
	}
	result := StoredFile{Key: input.key, URL: strings.TrimRight(s.PublicURL, "/") + "/" + input.key}
	head, err := s.Client.HeadObject(ctx, &oss.HeadObjectRequest{Bucket: oss.Ptr(s.Bucket), Key: oss.Ptr(input.key)})
	if err == nil {
		if head == nil || head.StatusCode != http.StatusOK {
			return StoredFile{}, errors.New("OSS HeadObject returned an unexpected status")
		}
		return result, nil
	}
	var serviceError *oss.ServiceError
	if !errors.As(err, &serviceError) || serviceError.StatusCode != http.StatusNotFound {
		return StoredFile{}, fmt.Errorf("OSS inspect object: %w", err)
	}
	request := &oss.PutObjectRequest{Bucket: oss.Ptr(s.Bucket), Key: oss.Ptr(input.key), Body: bytes.NewReader(input.data), ContentLength: oss.Ptr(int64(len(input.data))), ContentType: oss.Ptr(input.contentType), ForbidOverwrite: oss.Ptr("true")}
	if scope == "exports" {
		request.ContentDisposition = oss.Ptr(attachment(name))
	}
	uploaded, err := s.Client.PutObject(ctx, request)
	if err != nil {
		// A concurrent upload can win after our HEAD. Only this explicit conflict is reuse.
		if errors.As(err, &serviceError) && serviceError.StatusCode == http.StatusConflict && serviceError.Code == "FileAlreadyExists" {
			head, headErr := s.Client.HeadObject(ctx, &oss.HeadObjectRequest{Bucket: oss.Ptr(s.Bucket), Key: oss.Ptr(input.key)})
			if headErr != nil {
				return StoredFile{}, fmt.Errorf("OSS inspect concurrent upload: %w", headErr)
			}
			if head != nil && head.StatusCode == http.StatusOK {
				return result, nil
			}
		}
		return StoredFile{}, fmt.Errorf("OSS upload object: %w", err)
	}
	if uploaded == nil || uploaded.StatusCode != http.StatusOK {
		return StoredFile{}, errors.New("OSS PutObject returned an unexpected status")
	}
	result.Created = true
	return result, nil
}
func (s *OSSStore) Remove(ctx context.Context, file StoredFile) error {
	if !file.Created {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validStoredKey(file.Key) {
		return invalidFile()
	}
	if s.Client == nil || s.Bucket == "" {
		return errors.New("OSS storage configuration is incomplete or insecure")
	}
	result, err := s.Client.DeleteObject(ctx, &oss.DeleteObjectRequest{Bucket: oss.Ptr(s.Bucket), Key: oss.Ptr(file.Key)})
	if err != nil {
		return fmt.Errorf("OSS remove object: %w", err)
	}
	if result == nil || result.StatusCode != http.StatusNoContent && result.StatusCode != http.StatusOK {
		return errors.New("OSS DeleteObject returned an unexpected status")
	}
	return nil
}

// P-112 HTTP OSS HeadObject: only a 404 permits an upload; other errors preserve their identity.
// P-113 HTTP OSS PutObject: V4, bounded retries, TLS, validated body and MIME, atomic no-overwrite.
// P-114 HTTP OSS DeleteObject: compensation only; the service checks references before deletion.
