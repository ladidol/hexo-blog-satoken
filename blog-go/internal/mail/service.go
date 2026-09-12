package mail

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"log"
	"strings"
	"time"

	"blog-go/internal/platform"
)

// 用途：拥有直接邮件提交与结果的状态、数据访问及下列显式依赖。
type Service struct {
	db     *sql.DB
	cfg    platform.Config
	now    func() time.Time
	sender Sender
}

// 用途：构造直接邮件提交与结果，由 app 唯一装配依赖。
func New(db *sql.DB, cfg platform.Config, sender Sender, now func() time.Time) *Service {
	if sender == nil {
		sender = &SMTP{Config: cfg}
	}
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, cfg: cfg, sender: sender, now: now}
}

// 用途：传递一次验证码或评论通知，正文只存在于发送内存。
type Message struct {
	Purpose    string
	BusinessID *int64
	Recipient  string
	Subject    string
	Text       string
}

// 用途：区分 SMTP 接受、拒绝和结果不明，并追溯结果记录。
type Outcome struct {
	Status     string
	DeliveryID int64
	ErrorCode  string
}

// 用途：允许真实 SMTP 与测试受控服务注入同一提交契约。
type Sender interface {
	// 用途：执行当次 SMTP 提交，不自动重发，返回 accepted/failed/unknown。
	Submit(context.Context, Message) (string, error)
}

// 用途：同步提交并记录去敏感化结果。
func (s *Service) Send(ctx context.Context, m Message) (Outcome, error) {
	if err := validateMessage(m); err != nil {
		return Outcome{Status: "failed", ErrorCode: "invalid_message"}, err
	}
	transport := "controlled_test"
	if _, ok := s.sender.(*SMTP); ok {
		transport = "smtp"
	}
	submitContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, err := "failed", submitContext.Err()
	if err == nil {
		status, err = s.sender.Submit(submitContext, m)
	}
	// Only an unambiguous accepted result can activate a verification code.
	if (status != "accepted" && status != "failed" && status != "unknown") || (status == "accepted" && err != nil) {
		status = "unknown"
	}
	if status != "accepted" && err == nil {
		err = &submissionError{code: "smtp_" + status}
	}
	code := ""
	if err != nil {
		code = "smtp_" + status
		var submission *submissionError
		if errors.As(err, &submission) {
			code = submission.code
		} else if errors.Is(err, context.Canceled) {
			code = "smtp_canceled"
		} else if errors.Is(err, context.DeadlineExceeded) {
			code = "smtp_timeout"
		}
		if code == "smtp_disabled" {
			transport = "disabled"
		}
		err = &submissionError{code: code, cause: err}
	}
	outcome := Outcome{Status: status, ErrorCode: code}
	recipient, _ := parseMailbox(m.Recipient)
	at := strings.LastIndexByte(recipient.Address, '@')
	mask := recipient.Address[:1] + "***@" + recipient.Address[at+1:]
	// SMTP cannot be rolled back. Persist its final fact even if the HTTP caller
	// disconnected, using a fresh bounded context instead of a pending queue row.
	recordContext, recordCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer recordCancel()
	result, recordErr := s.db.ExecContext(recordContext, `INSERT INTO tb_mail_delivery
		(purpose,business_id,recipient_mask,status,transport_mode,error_code,create_time)
		VALUES(?,?,?,?,?,?,?)`, m.Purpose, m.BusinessID, mask, status, transport, code, s.now().Format(time.RFC3339Nano))
	if recordErr == nil {
		outcome.DeliveryID, recordErr = result.LastInsertId()
	}
	if recordErr != nil {
		if outcome.Status == "accepted" {
			outcome.Status = "unknown"
		}
		outcome.DeliveryID, outcome.ErrorCode = 0, "record_failed"
		// Provider/SQL errors can contain addresses, codes or credentials. Keep the
		// cause for errors.Is/As but expose only a stable diagnostic in logs/results.
		log.Printf("mail result recording failed: status=%s transport=%s error_code=record_failed", status, transport)
		return outcome, &submissionError{code: "record_failed", cause: errors.Join(err, recordErr)}
	}
	return outcome, err
}

// 用途：将本模块 schema.sql 交给唯一迁移器，业务逻辑不自动建表。
//
//go:embed schema.sql
var Schema string
