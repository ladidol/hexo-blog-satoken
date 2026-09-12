package mail

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"blog-go/internal/platform"
)

// 用途：保存实际 SMTP 主机、TLS 和认证配置，由运行环境提供。
type SMTP struct {
	Config platform.Config
}

// 用途：按有界 TLS SMTP 会话提交邮件并判定最终接受事实。
func (s *SMTP) Submit(ctx context.Context, m Message) (string, error) {
	if err := validateMessage(m); err != nil {
		return "failed", err
	}
	if s == nil {
		return "failed", &submissionError{code: "smtp_disabled"}
	}
	values := s.Config.Values
	host, port, mode := values["BLOG_SMTP_HOST"], values["BLOG_SMTP_PORT"], values["BLOG_SMTP_TLS_MODE"]
	username, password := values["BLOG_SMTP_USERNAME"], values["BLOG_SMTP_PASSWORD"]
	from, fromErr := parseMailbox(values["BLOG_SMTP_FROM"])
	portNumber, portErr := strconv.Atoi(port)
	if host == "" || strings.ContainsAny(host, "\r\n\t /\\") || portErr != nil || portNumber < 1 || portNumber > 65535 ||
		(mode != "tls" && mode != "starttls") || fromErr != nil || (username == "") != (password == "") {
		return "failed", &submissionError{code: "smtp_disabled"}
	}
	to, _ := parseMailbox(m.Recipient)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	failure := func(status string, cause error) (string, error) {
		code := "smtp_" + status
		if ctx.Err() != nil {
			cause = errors.Join(cause, ctx.Err())
		}
		var network net.Error
		if errors.Is(cause, context.DeadlineExceeded) || (errors.As(cause, &network) && network.Timeout()) {
			code = "smtp_timeout"
			// A socket deadline can fire just before context's timer is scheduled.
			cause = errors.Join(cause, context.DeadlineExceeded)
		} else if errors.Is(cause, context.Canceled) {
			code = "smtp_canceled"
		}
		return status, &submissionError{code: code, cause: cause}
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return failure("failed", err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return failure("failed", err)
	}
	// Closing the underlying socket also interrupts STARTTLS, AUTH and DATA.
	stopCancellation := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancellation()
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	var smtpConnection net.Conn = conn
	if mode == "tls" {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return failure("failed", err)
		}
		smtpConnection = secure
	}
	client, err := smtp.NewClient(smtpConnection, host)
	if err != nil {
		return failure("failed", err)
	}
	defer client.Close()
	if mode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return failure("failed", &submissionError{code: "smtp_tls_required"})
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return failure("failed", err)
		}
	}
	if username != "" {
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return failure("failed", err)
		}
	}
	// net/mail strips quoted local parts when parsing; restore their quoting in
	// the SMTP envelope as well as in the visible header.
	fromEnvelope := (&mail.Address{Address: from.Address}).String()
	toEnvelope := (&mail.Address{Address: to.Address}).String()
	if err := client.Mail(strings.TrimSuffix(strings.TrimPrefix(fromEnvelope, "<"), ">")); err != nil {
		return failure("failed", err)
	}
	if err := client.Rcpt(strings.TrimSuffix(strings.TrimPrefix(toEnvelope, "<"), ">")); err != nil {
		return failure("failed", err)
	}
	writer, err := client.Data()
	if err != nil {
		return failure("failed", err)
	}
	if err := writeMessage(writer, from, to, m); err != nil {
		// Do not close the DATA writer after a partial write: that would submit a
		// truncated message. The deferred connection close aborts the transaction.
		return failure("unknown", err)
	}
	if err := writer.Close(); err != nil {
		var response *textproto.Error
		if errors.As(err, &response) && response.Code >= 400 && response.Code < 600 {
			return failure("failed", err)
		}
		return failure("unknown", err)
	}
	// DATA's final 250 is the acceptance fact; later disconnect/QUIT failure
	// cannot retract it and is never a reason to retry automatically.
	return "accepted", nil
}

// Error text is safe to expose while Unwrap preserves cancellation and provider
// error identity for callers that need it. Raw responses must never be logged.
type submissionError struct {
	code  string
	cause error
}

func (e *submissionError) Error() string { return "mail submission: " + e.code }
func (e *submissionError) Unwrap() error { return e.cause }

func validateMessage(m Message) error {
	_, err := parseMailbox(m.Recipient)
	if err != nil || (m.Purpose != "verification" && m.Purpose != "comment") ||
		(m.BusinessID != nil && *m.BusinessID <= 0) || !validHeader(m.Subject) || !utf8.ValidString(m.Text) {
		return &submissionError{code: "invalid_message"}
	}
	return nil
}

func validHeader(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func parseMailbox(value string) (*mail.Address, error) {
	if !validHeader(value) || len(value) > 900 {
		return nil, &submissionError{code: "invalid_message"}
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address == nil || len(address.Address) > 254 || strings.LastIndexByte(address.Address, '@') <= 0 {
		return nil, &submissionError{code: "invalid_message"}
	}
	// UTF-8 display names and content use MIME; international envelope addresses
	// need a separate SMTPUTF8 contract and are not sent as invalid ASCII SMTP.
	for _, r := range address.Address {
		if r < 32 || r > 126 {
			return nil, &submissionError{code: "invalid_message"}
		}
	}
	return address, nil
}

func writeMessage(writer io.Writer, from, to *mail.Address, m Message) error {
	// Encode every subject (including long ASCII text) into bounded RFC 2047
	// words, folded only between words and never through a UTF-8 rune.
	var subject []string
	for rest := m.Subject; rest != ""; {
		n := len(rest)
		if n > 42 {
			n = 42
			for !utf8.RuneStart(rest[n]) {
				n--
			}
		}
		subject = append(subject, "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(rest[:n]))+"?=")
		rest = rest[n:]
	}
	_, err := fmt.Fprintf(writer, "From: %s\r\nTo: %s\r\nDate: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n",
		strings.ReplaceAll(from.String(), "?= =?", "?=\r\n =?"), strings.ReplaceAll(to.String(), "?= =?", "?=\r\n =?"),
		time.Now().Format(time.RFC1123Z), strings.Join(subject, "\r\n "))
	if err != nil {
		return err
	}
	encoded := quotedprintable.NewWriter(writer)
	if _, err := io.WriteString(encoded, m.Text); err != nil {
		return err
	}
	return encoded.Close()
}

// P-115 SMTP SMTP MAIL/RCPT/DATA
// 约束变化：新增Go直接调用替代MQ：TLS、头编码、超时；DATA接受=accepted、明确拒绝=failed、提交结果未判定=unknown；只记提交事实，不声明送达、不自动重投。 具体字段与状态解释见MODULE-010已确认执行边界。
