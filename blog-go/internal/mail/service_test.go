package mail_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	stdmail "net/mail"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"blog-go/internal/mail"
	"blog-go/internal/platform"
)

var trustedCertificate tls.Certificate

func certificate() (tls.Certificate, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{CommonName: "SMTP test only"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	root, err := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, root, err
}

func TestMain(m *testing.M) {
	cert, root, err := certificate()
	if err != nil {
		panic(err)
	}
	trustedCertificate = cert
	pool := x509.NewCertPool()
	pool.AddCert(root)
	// Trust is confined to this test binary; production retains system roots.
	if err := os.Setenv("GODEBUG", os.Getenv("GODEBUG")+",x509usefallbackroots=1"); err != nil {
		panic(err)
	}
	x509.SetFallbackRoots(pool)
	os.Exit(m.Run())
}

type senderFunc func(context.Context, mail.Message) (string, error)

func (f senderFunc) Submit(ctx context.Context, m mail.Message) (string, error) { return f(ctx, m) }

func message() mail.Message {
	id := int64(43)
	return mail.Message{Purpose: "verification", BusinessID: &id, Recipient: "person@example.test",
		Subject: "验证码与通知", Text: "验证码：123456\n第二行\n.结束"}
}

func database(t *testing.T) (*sql.DB, platform.Config) {
	t.Helper()
	cfg := platform.Config{DataDir: t.TempDir(), Values: map[string]string{}}
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := platform.Migrate(context.Background(), db, []string{mail.Schema}, nil); err != nil {
		t.Fatal(err)
	}
	return db, cfg
}

func TestServiceRecordsDirectResultsWithoutSecretsOrRetries(t *testing.T) {
	failure := errors.New("secret SMTP response person@example.test 123456 password")
	for _, tc := range []struct {
		name, submitted, want string
		err                   error
	}{
		{"accepted", "accepted", "accepted", nil}, {"rejected", "failed", "failed", failure},
		{"uncertain", "unknown", "unknown", failure}, {"failure without error", "failed", "failed", nil},
		{"unknown without error", "unknown", "unknown", nil}, {"conflicting outcome", "accepted", "unknown", failure},
		{"invalid outcome", "delivered", "unknown", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, cfg := database(t)
			calls := 0
			when := time.Date(2026, 9, 12, 8, 15, 0, 0, time.UTC)
			service := mail.New(db, cfg, senderFunc(func(ctx context.Context, got mail.Message) (string, error) {
				calls++
				var count int
				if err := db.QueryRow("SELECT count(*) FROM tb_mail_delivery").Scan(&count); err != nil || count != 0 {
					t.Fatalf("created pending result before submission: %d %v", count, err)
				}
				if got.Recipient != message().Recipient || got.Text != message().Text {
					t.Fatal("submission lost message content")
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("submission has no ten-second budget")
				}
				return tc.submitted, tc.err
			}), func() time.Time { return when })
			outcome, err := service.Send(context.Background(), message())
			if calls != 1 || outcome.Status != tc.want || outcome.DeliveryID <= 0 || (err == nil) != (tc.want == "accepted") {
				t.Fatalf("outcome=%+v err=%v calls=%d", outcome, err, calls)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatal("lost original failure identity")
			}
			if err != nil && strings.Contains(err.Error(), "123456") {
				t.Fatal("error exposed SMTP secrets")
			}
			var purpose, recipient, status, transport, code, created string
			var businessID int64
			if err := db.QueryRow("SELECT purpose,business_id,recipient_mask,status,transport_mode,error_code,create_time FROM tb_mail_delivery WHERE id=?", outcome.DeliveryID).
				Scan(&purpose, &businessID, &recipient, &status, &transport, &code, &created); err != nil {
				t.Fatal(err)
			}
			if purpose != "verification" || businessID != 43 || status != tc.want || transport != "controlled_test" || code != outcome.ErrorCode || !strings.HasPrefix(created, "2026-09-12T08:15:00") {
				t.Fatalf("incorrect audit row: %s %d %s %s %s %s", purpose, businessID, status, transport, code, created)
			}
			if recipient == message().Recipient || !strings.Contains(recipient, "*") || strings.Contains(recipient+code, "123456") || strings.Contains(code, "password") {
				t.Fatal("audit row contains sensitive content")
			}
			if (code == "") != (tc.want == "accepted") {
				t.Fatalf("inaccurate diagnostic code: %q", code)
			}
		})
	}
}

func TestServiceMissingConfigurationIsPersistedAndCannotEnableFakeSending(t *testing.T) {
	db, cfg := database(t)
	cfg.Values["BLOG_SMTP_TLS_MODE"] = "controlled_test"
	service := mail.New(db, cfg, nil, nil)
	outcome, err := service.Send(context.Background(), message())
	if err == nil || outcome.Status != "failed" || outcome.DeliveryID <= 0 || outcome.ErrorCode == "" {
		t.Fatalf("disabled SMTP reported success: %+v %v", outcome, err)
	}
	var transport string
	if err := db.QueryRow("SELECT transport_mode FROM tb_mail_delivery WHERE id=?", outcome.DeliveryID).Scan(&transport); err != nil || transport != "disabled" {
		t.Fatalf("disabled configuration not recorded: %s %v", transport, err)
	}
}

func TestServiceRejectsInvalidMessagesBeforeSending(t *testing.T) {
	db, cfg := database(t)
	service := mail.New(db, cfg, senderFunc(func(context.Context, mail.Message) (string, error) {
		t.Fatal("invalid message reached SMTP")
		return "accepted", nil
	}), nil)
	for _, modify := range []func(*mail.Message){
		func(m *mail.Message) { m.Subject += "\r\nBcc: injected@example.test" },
		func(m *mail.Message) { m.Recipient += "\nRCPT TO:injected@example.test" },
		func(m *mail.Message) { m.Recipient = "one@example.test,two@example.test" },
		func(m *mail.Message) { m.Recipient = "" },
		func(m *mail.Message) { m.Purpose = "arbitrary" },
		func(m *mail.Message) { id := int64(-1); m.BusinessID = &id },
		func(m *mail.Message) { m.Text = string([]byte{0xff}) },
	} {
		m := message()
		modify(&m)
		outcome, err := service.Send(context.Background(), m)
		if err == nil || outcome.Status != "failed" {
			t.Fatalf("invalid message accepted: %+v %v", outcome, err)
		}
	}
}

func TestServicePreservesAcceptedFactAfterCancellationAndReportsRecordingFailure(t *testing.T) {
	db, cfg := database(t)
	ctx, cancel := context.WithCancel(context.Background())
	service := mail.New(db, cfg, senderFunc(func(context.Context, mail.Message) (string, error) {
		cancel()
		return "accepted", nil
	}), nil)
	outcome, err := service.Send(ctx, message())
	if err != nil || outcome.Status != "accepted" || outcome.DeliveryID <= 0 {
		t.Fatalf("caller cancellation erased confirmed submission: %+v %v", outcome, err)
	}
	if _, err := db.Exec("CREATE TRIGGER reject_audit BEFORE INSERT ON tb_mail_delivery BEGIN SELECT RAISE(FAIL,'private database diagnostic 123456'); END"); err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&diagnostic)
	defer log.SetOutput(previous)
	calls := 0
	service = mail.New(db, cfg, senderFunc(func(context.Context, mail.Message) (string, error) {
		calls++
		return "accepted", nil
	}), nil)
	outcome, err = service.Send(context.Background(), message())
	if err == nil || outcome.Status != "unknown" || outcome.DeliveryID != 0 || outcome.ErrorCode == "" || calls != 1 {
		t.Fatalf("unrecorded accepted result: %+v %v calls=%d", outcome, err, calls)
	}
	if diagnostic.Len() == 0 || strings.Contains(diagnostic.String()+err.Error(), "123456") || strings.Contains(diagnostic.String(), message().Recipient) {
		t.Fatal("recording failure lacks a safe local diagnostic")
	}
}

func TestServiceCanceledAndConcurrentAttemptsRemainPersistent(t *testing.T) {
	db, cfg := database(t)
	var calls atomic.Int64
	service := mail.New(db, cfg, senderFunc(func(context.Context, mail.Message) (string, error) {
		calls.Add(1)
		return "accepted", nil
	}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := service.Send(ctx, message())
	if got.Status != "failed" || !errors.Is(err, context.Canceled) || calls.Load() != 0 || got.DeliveryID == 0 {
		t.Fatalf("canceled request attempted SMTP or lost audit: %+v %v", got, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := message()
			m.Purpose, m.BusinessID = "comment", nil
			outcome, err := service.Send(context.Background(), m)
			if err != nil || outcome.Status != "accepted" || outcome.DeliveryID <= 0 {
				t.Errorf("concurrent result: %+v %v", outcome, err)
			}
		}()
	}
	wg.Wait()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err := reopened.QueryRow("SELECT count(*) FROM tb_mail_delivery WHERE status='accepted' AND purpose='comment' AND business_id IS NULL").Scan(&count); err != nil || count != 12 || calls.Load() != 12 {
		t.Fatalf("persistent attempts=%d calls=%d err=%v", count, calls.Load(), err)
	}
}

type smtpEvidence struct {
	commands []string
	data     string
	err      error
}

// One accepted connection represents one actual submission attempt, with no retries.
func smtpServer(t *testing.T, mode, behavior string, dataReceived ...chan<- struct{}) (platform.Config, <-chan smtpEvidence) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	cfg := platform.Config{Values: map[string]string{
		"BLOG_SMTP_HOST": "127.0.0.1", "BLOG_SMTP_PORT": port, "BLOG_SMTP_TLS_MODE": mode,
		"BLOG_SMTP_FROM": "sender@example.test", "BLOG_SMTP_USERNAME": "sender", "BLOG_SMTP_PASSWORD": "test-password",
	}}
	cert := trustedCertificate
	if behavior == "untrusted" {
		cert, _, err = certificate()
		if err != nil {
			t.Fatal(err)
		}
	}
	result := make(chan smtpEvidence, 1)
	go func() {
		var evidence smtpEvidence
		defer func() { result <- evidence }()
		conn, err := listener.Accept()
		if err != nil {
			evidence.err = err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if behavior == "stall default timeout" {
			_ = conn.SetDeadline(time.Now().Add(12 * time.Second))
		}
		tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		if mode == "tls" {
			secure := tls.Server(conn, tlsConfig)
			if err := secure.Handshake(); err != nil {
				return
			}
			conn = secure
		}
		wire := textproto.NewConn(conn)
		if behavior == "stall greeting" {
			_, _ = wire.ReadLine()
			return
		}
		_ = wire.PrintfLine("220 SMTP test service")
		secured := mode == "tls"
		for {
			line, err := wire.ReadLine()
			if err != nil {
				return
			}
			evidence.commands = append(evidence.commands, line)
			switch {
			case strings.HasPrefix(line, "EHLO "):
				if !secured && behavior != "no starttls" {
					_ = wire.PrintfLine("250-test\r\n250 STARTTLS")
				} else {
					_ = wire.PrintfLine("250-test\r\n250 AUTH PLAIN")
				}
			case line == "STARTTLS":
				_ = wire.PrintfLine("220 begin TLS")
				secure := tls.Server(conn, tlsConfig)
				if err := secure.Handshake(); err != nil {
					return
				}
				conn, secured = secure, true
				wire = textproto.NewConn(conn)
			case strings.HasPrefix(line, "AUTH PLAIN "):
				auth, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
				if !secured || err != nil || string(auth) != "\x00sender\x00test-password" {
					evidence.err = errors.New("credentials were incorrect or sent before TLS")
					return
				}
				if behavior == "reject auth" {
					_ = wire.PrintfLine("535 private provider response 123456")
				} else {
					_ = wire.PrintfLine("235 authenticated")
				}
			case strings.HasPrefix(line, "MAIL FROM:"):
				_ = wire.PrintfLine("250 sender accepted")
			case strings.HasPrefix(line, "RCPT TO:"):
				if behavior == "reject rcpt" {
					_ = wire.PrintfLine("550 private provider response 123456")
				} else {
					_ = wire.PrintfLine("250 recipient accepted")
				}
			case line == "DATA":
				if behavior == "reject data" {
					_ = wire.PrintfLine("554 private provider response 123456")
					continue
				}
				_ = wire.PrintfLine("354 send content")
				if behavior == "disconnect during data" {
					return
				}
				data, err := io.ReadAll(wire.DotReader())
				if err != nil {
					evidence.err = err
					return
				}
				evidence.data = string(data)
				for _, ready := range dataReceived {
					close(ready)
				}
				switch behavior {
				case "disconnect":
					return
				case "stall result", "stall default timeout":
					_, _ = wire.ReadLine()
					return
				case "reject result":
					_ = wire.PrintfLine("554 private provider response 123456")
				case "invalid result":
					_ = wire.PrintfLine("354 unexpected intermediate reply")
				default:
					_ = wire.PrintfLine("250 accepted for submission")
					return // Post-accept disconnect must not undo accepted.
				}
			case line == "QUIT":
				_ = wire.PrintfLine("221 bye")
				return
			case line == "*":
				_ = wire.PrintfLine("501 authentication aborted")
			default:
				evidence.err = fmt.Errorf("unexpected command: %s", line)
				return
			}
		}
	}()
	return cfg, result
}

func TestSMTPSubmissionTLSAndSTARTTLS(t *testing.T) {
	for _, mode := range []string{"tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			cfg, evidence := smtpServer(t, mode, "accepted")
			m := message()
			m.Subject = strings.Repeat("验证码通知", 30)
			status, err := (&mail.SMTP{Config: cfg}).Submit(context.Background(), m)
			if err != nil || status != "accepted" {
				t.Fatalf("real TLS submission failed: %s %v", status, err)
			}
			got := <-evidence
			if got.err != nil || got.data == "" {
				t.Fatalf("SMTP service did not receive mail: %+v", got)
			}
			parsed, err := stdmail.ReadMessage(strings.NewReader(got.data))
			if err != nil {
				t.Fatal(err)
			}
			subject, err := (&mime.WordDecoder{}).DecodeHeader(parsed.Header.Get("Subject"))
			if err != nil || subject != m.Subject || parsed.Header.Get("From") != "<sender@example.test>" || parsed.Header.Get("To") != "<person@example.test>" {
				t.Fatalf("invalid RFC headers: %s %v %#v", subject, err, parsed.Header)
			}
			if _, err := stdmail.ParseDate(parsed.Header.Get("Date")); err != nil {
				t.Fatal("missing valid Date header")
			}
			if parsed.Header.Get("Content-Transfer-Encoding") != "quoted-printable" || !strings.Contains(strings.ToLower(parsed.Header.Get("Content-Type")), "utf-8") {
				t.Fatal("UTF-8 text lacks MIME encoding")
			}
			body, err := io.ReadAll(quotedprintable.NewReader(parsed.Body))
			if err != nil || strings.ReplaceAll(strings.TrimSuffix(string(body), "\n"), "\r\n", "\n") != m.Text {
				t.Fatalf("body was not preserved: %q %v", body, err)
			}
			for _, line := range strings.Split(got.data, "\n") {
				if len(line) > 998 {
					t.Fatal("RFC line limit exceeded")
				}
			}
		})
	}
}

func TestSMTPClassifiesRejectedUncertainAndTLSFailures(t *testing.T) {
	for _, tc := range []struct{ behavior, mode, want string }{
		{"reject auth", "tls", "failed"}, {"reject rcpt", "tls", "failed"},
		{"reject data", "tls", "failed"}, {"reject result", "tls", "failed"},
		{"disconnect", "tls", "unknown"}, {"invalid result", "tls", "unknown"},
		{"disconnect during data", "tls", "unknown"},
		{"untrusted", "tls", "failed"}, {"untrusted", "starttls", "failed"},
		{"no starttls", "starttls", "failed"}, {"wrong hostname", "tls", "failed"},
	} {
		t.Run(tc.behavior+tc.mode, func(t *testing.T) {
			cfg, evidence := smtpServer(t, tc.mode, tc.behavior)
			if tc.behavior == "wrong hostname" {
				cfg.Values["BLOG_SMTP_HOST"] = "localhost"
			}
			m := message()
			if tc.behavior == "disconnect during data" {
				m.Text = strings.Repeat("large UTF-8 内容", 100000)
			}
			status, err := (&mail.SMTP{Config: cfg}).Submit(context.Background(), m)
			if status != tc.want || err == nil || strings.Contains(err.Error(), "123456") {
				t.Fatalf("wrong or unsafe outcome: %s %v", status, err)
			}
			got := <-evidence
			if got.err != nil {
				t.Fatal(got.err)
			}
			if tc.behavior == "untrusted" || tc.behavior == "no starttls" || tc.behavior == "wrong hostname" {
				for _, command := range got.commands {
					if strings.HasPrefix(command, "AUTH") || strings.HasPrefix(command, "MAIL") {
						t.Fatal("sent credentials/content without trusted TLS")
					}
				}
			}
		})
	}
}

func TestSMTPDeadlinesAndCancellationCloseConnection(t *testing.T) {
	for _, tc := range []struct{ behavior, want string }{{"stall greeting", "failed"}, {"stall result", "unknown"}} {
		t.Run(tc.behavior, func(t *testing.T) {
			cfg, evidence := smtpServer(t, "tls", tc.behavior)
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			started := time.Now()
			status, err := (&mail.SMTP{Config: cfg}).Submit(ctx, message())
			if status != tc.want || !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
				t.Fatalf("deadline behavior: %s %v duration=%s", status, err, time.Since(started))
			}
			<-evidence
		})
	}
}

func TestSMTPDefaultSubmissionBudgetIsTenSeconds(t *testing.T) {
	cfg, evidence := smtpServer(t, "tls", "stall default timeout")
	started := time.Now()
	status, err := (&mail.SMTP{Config: cfg}).Submit(context.Background(), message())
	if status != "unknown" || !errors.Is(err, context.DeadlineExceeded) || time.Since(started) < 9500*time.Millisecond || time.Since(started) > 11500*time.Millisecond {
		t.Fatalf("default submission budget: %s %v elapsed=%s", status, err, time.Since(started))
	}
	<-evidence
}

func TestSMTPActiveCancellationInterruptsWaitingForAcceptance(t *testing.T) {
	ready := make(chan struct{})
	cfg, evidence := smtpServer(t, "starttls", "stall result", ready)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		status string
		err    error
	}
	finished := make(chan result, 1)
	go func() {
		status, err := (&mail.SMTP{Config: cfg}).Submit(ctx, message())
		finished <- result{status, err}
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("submission did not reach DATA")
	}
	cancel()
	select {
	case got := <-finished:
		if got.status != "unknown" || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled acceptance: %s %v", got.status, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt SMTP read")
	}
	<-evidence
}

func TestServiceActualSMTPUsesSMTPAuditAndPreservesQuotedEnvelope(t *testing.T) {
	db, _ := database(t)
	cfg, evidence := smtpServer(t, "starttls", "accepted")
	m := message()
	m.Recipient = `"person name"@example.test`
	outcome, err := mail.New(db, cfg, nil, nil).Send(context.Background(), m)
	if outcome.Status != "accepted" || outcome.DeliveryID <= 0 || err != nil {
		t.Fatalf("real SMTP submission was not recorded: %+v %v", outcome, err)
	}
	got := <-evidence
	if got.err != nil || !strings.Contains(strings.Join(got.commands, "\n"), `RCPT TO:<"person name"@example.test>`) {
		t.Fatalf("invalid quoted envelope: %+v", got)
	}
	var mode string
	if err := db.QueryRow("SELECT transport_mode FROM tb_mail_delivery WHERE id=?", outcome.DeliveryID).Scan(&mode); err != nil || mode != "smtp" {
		t.Fatalf("incorrect transport audit: %s %v", mode, err)
	}
}

func TestSMTPInvalidHeadersAndConnectionFailure(t *testing.T) {
	for _, key := range []string{"BLOG_SMTP_FROM", "BLOG_SMTP_HOST"} {
		t.Run(key, func(t *testing.T) {
			cfg := platform.Config{Values: map[string]string{
				"BLOG_SMTP_HOST": "127.0.0.1", "BLOG_SMTP_PORT": "1", "BLOG_SMTP_TLS_MODE": "tls", "BLOG_SMTP_FROM": "sender@example.test",
			}}
			cfg.Values[key] += "\r\nBcc: secret@example.test"
			status, err := (&mail.SMTP{Config: cfg}).Submit(context.Background(), message())
			if status != "failed" || err == nil || strings.Contains(err.Error(), "secret@example.test") {
				t.Fatalf("unsafe configuration accepted: %s %v", status, err)
			}
		})
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := platform.Config{Values: map[string]string{
		"BLOG_SMTP_HOST": "127.0.0.1", "BLOG_SMTP_PORT": port, "BLOG_SMTP_TLS_MODE": "tls", "BLOG_SMTP_FROM": "sender@example.test",
	}}
	status, err := (&mail.SMTP{Config: cfg}).Submit(context.Background(), message())
	if status != "failed" || err == nil {
		t.Fatalf("connection failure hidden: %s %v", status, err)
	}
}
