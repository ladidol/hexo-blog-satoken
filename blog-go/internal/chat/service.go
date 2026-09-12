package chat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"blog-go/internal/analytics"
	"blog-go/internal/community"
	"blog-go/internal/files"
	"blog-go/internal/platform"
	"golang.org/x/net/html"
)

// 用途：拥有聊天室连接与消息所有权的状态、数据访问及下列显式依赖。
type Service struct {
	db        *sql.DB
	cfg       platform.Config
	now       func() time.Time
	files     *files.Service
	analytics *analytics.Service
	resolver  func(context.Context, string) (platform.Actor, error)
	hub       *Hub
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	stopped   chan struct{}
	mu        sync.Mutex
	closing   bool
	workers   sync.WaitGroup
}

// 用途：构造聊天室连接与消息所有权，由 app 唯一装配依赖。
func New(db *sql.DB, cfg platform.Config, files *files.Service, analytics *analytics.Service, resolver func(context.Context, string) (platform.Actor, error), now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.TimeZone == nil {
		cfg.TimeZone = time.UTC
	}
	owned, cancel := context.WithCancel(context.Background())
	s := &Service{db: db, cfg: cfg, now: now, files: files, analytics: analytics, resolver: resolver, ctx: owned, cancel: cancel, done: make(chan struct{}), stopped: make(chan struct{}), hub: &Hub{Register: make(chan *Client), Unregister: make(chan *Client), Broadcast: make(chan Frame), Clients: make(map[*Client]struct{}), work: make(chan hubWork)}}
	go func() { defer close(s.done); _ = s.hub.Run(owned) }()
	return s
}

// 用途：清理连接与写协程，实现本机服务的优雅停机。
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closing {
		s.closing = true
		s.cancel()
		go func() { <-s.done; s.workers.Wait(); close(s.stopped) }()
	}
	s.mu.Unlock()
	select {
	case <-s.stopped:
		return nil
	default:
	}
	select {
	case <-s.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// 用途：处理文本、归属撤回和心跳帧；持久成功后广播。
func (s *Service) HandleFrame(ctx context.Context, client *Client, frame Frame) error {
	if client == nil || client.VisitorKey == "" {
		return policy("缺少浏览会话")
	}
	return s.execute(ctx, func(ctx context.Context) error {
		if _, exists := s.hub.Clients[client]; !exists {
			return policy("连接已失效")
		}
		switch frame.Type {
		case 3:
			var data struct{ Nickname, Avatar, Content string }
			if err := frameData(frame.Data, &data); err != nil {
				return err
			}
			if utf8.RuneCountInString(data.Content) > 1000 {
				return policy("聊天内容不能超过 1000 字")
			}
			content, err := community.SanitizeHTML(data.Content)
			if err != nil {
				return policy("聊天内容不正确")
			}
			author, owner, err := s.author(ctx, client.VisitorKey, data.Nickname, data.Avatar)
			if err != nil {
				return err
			}
			record, err := s.saveRecord(ctx, author, owner, client.ipAddress, client.ipSource, content, 3)
			if err != nil {
				return err
			}
			s.hub.broadcast(Frame{Type: 3, Data: record})
			return nil
		case 4:
			var data struct{ ID int64 }
			if err := frameData(frame.Data, &data); err != nil {
				return err
			}
			if data.ID <= 0 {
				return policy("撤回目标无效")
			}
			_, owner, err := s.currentOwner(ctx, client.VisitorKey)
			if err != nil {
				return err
			}
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			var found string
			var typ int
			err = tx.QueryRowContext(ctx, "SELECT owner_key,type FROM tb_chat_record WHERE id=?", data.ID).Scan(&found, &typ)
			if errors.Is(err, sql.ErrNoRows) {
				return policy("消息不存在")
			}
			if err != nil {
				return err
			}
			if found != owner {
				return policy("只能撤回自己的消息")
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM tb_chat_record WHERE id=? AND owner_key=?", data.ID, owner); err != nil {
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			s.hub.broadcast(Frame{Type: 4, Data: platform.Object{"id": data.ID, "isVoice": typ == 5}})
			return nil
		case 6:
			if !s.hub.send(client, Frame{Type: 6, Data: "pong"}) {
				s.hub.online()
			}
			return nil
		default:
			return policy("不允许的消息类型")
		}
	})
}

// 用途：承接原 POST /voice 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SendVoice(ctx context.Context, in platform.Input) (any, error) {
	if !s.beginWork() {
		return nil, closed()
	}
	defer s.workers.Done()
	if in.VisitorKey == "" || in.Form.Get("type") != "5" || len(in.Files) != 1 || in.Files[0] == nil {
		return nil, invalid("语音参数不正确")
	}
	owned, cancel := context.WithTimeout(ctx, 30*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	header := in.Files[0]
	body, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer body.Close()
	file, err := s.files.Put(owned, "voice", header.Filename, header.Header.Get("Content-Type"), body)
	if err != nil {
		return nil, err
	}
	ip := in.IP
	if in.Request != nil {
		ip = remoteIP(in.Request.RemoteAddr)
	}
	source := ""
	if s.analytics != nil {
		source = s.analytics.LookupIP(owned, ip)
	}
	err = s.execute(owned, func(ctx context.Context) error {
		author, owner, err := s.author(ctx, in.VisitorKey, in.Form.Get("nickname"), in.Form.Get("avatar"))
		if err != nil {
			return err
		}
		record, err := s.saveRecord(ctx, author, owner, ip, source, file.URL, 5)
		if err != nil {
			return err
		}
		s.hub.broadcast(Frame{Type: 5, Data: record})
		return nil
	})
	if err != nil {
		var protocol *protocolError
		if errors.As(err, &protocol) {
			err = errors.Join(invalid(protocol.message), err)
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return nil, errors.Join(err, s.files.Discard(cleanup, file))
	}
	return nil, nil
}

type protocolError struct {
	code    int
	message string
}

func (e *protocolError) Error() string { return e.message }
func policy(message string) error      { return &protocolError{code: 1008, message: message} }
func malformed() error                 { return &protocolError{code: 1007, message: "消息格式不正确"} }
func invalid(message string) error {
	return &platform.Error{Code: platform.CodeInvalid, Message: message}
}
func closed() error { return &platform.Error{Code: platform.CodeFail, Message: "聊天室已关闭"} }
func (s *Service) beginWork() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.workers.Add(1)
	return true
}
func (s *Service) execute(ctx context.Context, run func(context.Context) error) error {
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	work := hubWork{ctx: op, run: run, reply: make(chan error, 1)}
	select {
	case <-s.ctx.Done():
		return closed()
	default:
	}
	select {
	case s.hub.work <- work:
	case <-op.Done():
		return op.Err()
	case <-s.done:
		return closed()
	}
	// Once accepted, wait for the commit decision. Returning only on caller
	// cancellation could report failure after a successful commit/broadcast.
	select {
	case err := <-work.reply:
		return err
	case <-s.done:
		select {
		case err := <-work.reply:
			return err
		default:
			return closed()
		}
	}
}
func frameData(value any, target any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, target) != nil {
		return malformed()
	}
	return nil
}
func (s *Service) currentOwner(ctx context.Context, visitor string) (platform.Actor, string, error) {
	if visitor == "" {
		return platform.Actor{}, "", policy("缺少浏览会话")
	}
	var actor platform.Actor
	var err error
	if s.resolver != nil {
		actor, err = s.resolver(ctx, visitor)
		if err != nil {
			return actor, "", err
		}
	}
	if actor.LoggedIn {
		if actor.UserInfoID <= 0 {
			return actor, "", policy("登录身份无效")
		}
		return actor, "u:" + strconv.FormatInt(actor.UserInfoID, 10), nil
	}
	hash := sha256.Sum256([]byte(visitor))
	return platform.Actor{}, "g:" + hex.EncodeToString(hash[:]), nil
}
func (s *Service) author(ctx context.Context, visitor, nickname, avatar string) (platform.Actor, string, error) {
	actor, owner, err := s.currentOwner(ctx, visitor)
	if err != nil {
		return actor, owner, err
	}
	if actor.LoggedIn {
		return actor, owner, nil
	}
	clean, err := community.SanitizeHTML(nickname)
	if err != nil {
		return actor, "", policy("游客昵称不正确")
	}
	tokens := html.NewTokenizer(strings.NewReader(clean))
	var text strings.Builder
	for {
		typ := tokens.Next()
		if typ == html.ErrorToken {
			if tokens.Err() != io.EOF {
				return actor, "", malformed()
			}
			break
		}
		if typ == html.TextToken {
			text.Write(tokens.Text())
		}
	}
	nickname = strings.TrimSpace(text.String())
	if nickname == "" || utf8.RuneCountInString(nickname) > 100 || !safeAvatar(avatar) {
		return actor, "", policy("游客资料不正确")
	}
	actor.Nickname, actor.Avatar = nickname, avatar
	return actor, owner, nil
}
func safeAvatar(value string) bool {
	if value == "" || strings.Contains(value, "\\") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.User != nil || u.Opaque != "" {
		return false
	}
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && u.Host == "" {
		return true
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}
func (s *Service) saveRecord(ctx context.Context, actor platform.Actor, owner, ip, source, content string, typ int) (platform.Object, error) {
	var user any
	if actor.LoggedIn {
		user = actor.UserInfoID
	}
	at := s.now().In(s.cfg.TimeZone).Format("2006-01-02T15:04:05")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "INSERT INTO tb_chat_record(user_id,nickname,avatar,content,ip_address,ip_source,type,create_time,owner_key) VALUES(?,?,?,?,?,?,?,?,?)", user, actor.Nickname, actor.Avatar, content, ip, source, typ, at, owner)
	if err != nil {
		return nil, fmt.Errorf("save chat record: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return platform.Object{"id": id, "userId": user, "nickname": actor.Nickname, "avatar": actor.Avatar, "content": content, "ipAddress": ip, "ipSource": source, "type": typ, "createTime": at, "updateTime": nil}, nil
}
func (s *Service) history(ctx context.Context) ([]platform.Object, error) {
	at := s.now().In(s.cfg.TimeZone)
	rows, err := s.db.QueryContext(ctx, "SELECT id,user_id,nickname,avatar,content,ip_address,ip_source,type,create_time,update_time FROM tb_chat_record WHERE create_time>=? AND create_time<=? AND type IN (3,5) ORDER BY create_time,id", at.Add(-12*time.Hour).Format("2006-01-02T15:04:05"), at.Format("2006-01-02T15:04:05"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []platform.Object{}
	for rows.Next() {
		var id int64
		var uid sql.NullInt64
		var nickname, avatar, content, ip, source, created string
		var typ int
		var updated sql.NullString
		if err := rows.Scan(&id, &uid, &nickname, &avatar, &content, &ip, &source, &typ, &created, &updated); err != nil {
			return nil, err
		}
		var user, update any
		if uid.Valid {
			user = uid.Int64
		}
		if updated.Valid {
			update = updated.String
		}
		records = append(records, platform.Object{"id": id, "userId": user, "nickname": nickname, "avatar": avatar, "content": content, "ipAddress": ip, "ipSource": source, "type": typ, "createTime": created, "updateTime": update})
	}
	return records, rows.Err()
}

// 用途：将本模块 schema.sql 交给唯一迁移器，业务逻辑不自动建表。
//
//go:embed schema.sql
var Schema string

// Public protocol projections use platform.Object; no database row is serialized directly.
// Integer/Long: JSON number; Date: Unix milliseconds; LocalDateTime: yyyy-MM-ddTHH:mm:ss.
// Optional fields retain null; lists/sets are arrays. UserDetailDTO never includes password.
// ChatRecord: id:Integer, userId:Integer, nickname:String, avatar:String, content:String, ipAddress:String, ipSource:String, type:Integer, createTime:LocalDateTime, updateTime:LocalDateTime; owner_key stays private.
// ChatRecordDTO: chatRecordList:ChatRecord[], ipAddress:String, ipSource:String
// RecallMessageDTO: id:Integer, isVoice:Boolean
// WebsocketMessageDTO: type:Integer, data:Object
// VoiceVO: type:Integer, file:MultipartFile, userId:Integer, nickname:String, avatar:String, content:String, createTime:Date, ipAddress:String, ipSource:String
