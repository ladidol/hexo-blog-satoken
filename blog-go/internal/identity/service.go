package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	stdmail "net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"blog-go/internal/analytics"
	"blog-go/internal/engagement"
	"blog-go/internal/files"
	"blog-go/internal/mail"
	"blog-go/internal/platform"
	"blog-go/internal/site"
	"blog-go/internal/social"
)

// 用途：拥有账号、资料与管理授权的状态、数据访问及下列显式依赖。
type Service struct {
	db         *sql.DB
	cfg        platform.Config
	now        func() time.Time
	files      *files.Service
	mail       *mail.Service
	social     *social.Service
	site       *site.Service
	analytics  *analytics.Service
	engagement *engagement.Service
	sessions   *SessionStore
	codes      *CodeStore
}

// 用途：构造账号、资料与管理授权，由 app 唯一装配依赖。
func New(db *sql.DB, cfg platform.Config, files *files.Service, mail *mail.Service, social *social.Service, site *site.Service, analytics *analytics.Service, engagement *engagement.Service, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.TimeZone == nil {
		cfg.TimeZone = time.UTC
	}
	return &Service{db: db, cfg: cfg, now: now, files: files, mail: mail, social: social, site: site, analytics: analytics, engagement: engagement, sessions: &SessionStore{byToken: make(map[string]Session), byVisitor: make(map[string]string), devices: make(map[string][2]string)}, codes: &CodeStore{entries: make(map[string]CodeEntry)}}
}

// 用途：验证码哈希与有效期用于邮箱验证和一次性消费。
type CodeEntry struct {
	Hash      [32]byte
	ExpiresAt time.Time
}

// 用途：并发保护邮箱验证码校验、业务提交与消费的原子次序。
type CodeStore struct {
	mu       sync.Mutex
	entries  map[string]CodeEntry
	requests map[string]time.Time
}

// 用途：承接原 POST /login 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) Login(ctx context.Context, in platform.Input) (any, error) {
	var payload struct{ Username, Password string }
	if err := decode(in, &payload); err != nil {
		return nil, err
	}
	username := strings.ToLower(strings.TrimSpace(payload.Username))
	if username == "" || payload.Password == "" {
		return nil, invalid("账号和密码不能为空")
	}
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM tb_user_auth WHERE username=? COLLATE NOCASE AND login_type=1", username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, denied("账号或密码不正确")
	}
	if err != nil {
		return nil, err
	}
	return s.loginAccount(ctx, in, id, &payload.Password, nil)
}

// 用途：承接原 ANY /logout 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) Logout(ctx context.Context, in platform.Input) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.sessions.mu.Lock()
	s.removeTokenLocked(requestToken(in.Request))
	s.sessions.mu.Unlock()
	s.setTokenCookie(in, "", -1)
	return nil, nil
}

// 用途：承接原 POST /users/oauth/weibo 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) WeiboLogin(ctx context.Context, in platform.Input) (any, error) {
	var p struct{ Code string }
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	verified, err := s.social.VerifyWeibo(ctx, p.Code)
	if err != nil {
		return nil, err
	}
	return s.socialLogin(ctx, in, verified)
}

// 用途：承接原 POST /users/oauth/qq 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) QqLogin(ctx context.Context, in platform.Input) (any, error) {
	var p struct{ OpenID, AccessToken string }
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	verified, err := s.social.VerifyQQ(ctx, p.OpenID, p.AccessToken)
	if err != nil {
		return nil, err
	}
	return s.socialLogin(ctx, in, verified)
}

// 用途：承接原 GET /users/code 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SendCode(ctx context.Context, in platform.Input) (any, error) {
	email, err := normalizeEmail(in.Query.Get("username"))
	if err != nil {
		return nil, err
	}
	s.codes.mu.Lock()
	defer s.codes.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.codes.requests == nil {
		s.codes.requests = make(map[string]time.Time)
	}
	for key, expiry := range s.codes.requests {
		if !s.now().Before(expiry) {
			delete(s.codes.requests, key)
		}
	}
	for key, entry := range s.codes.entries {
		if !s.now().Before(entry.ExpiresAt) {
			delete(s.codes.entries, key)
		}
	}
	key := "GET /users/code\x00" + in.IP
	if s.now().Before(s.codes.requests[key]) {
		return nil, denied("验证码请求过于频繁，请稍后重试")
	}
	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return nil, err
	}
	code := fmt.Sprintf("%06d", value.Int64())
	s.codes.requests[key] = s.now().Add(time.Minute)
	if s.mail == nil {
		return nil, denied("邮件发送未配置")
	}
	outcome, err := s.mail.Send(ctx, mail.Message{Purpose: "verification", Recipient: email, Subject: "博客验证码", Text: "您的验证码为 " + code + "，有效期 15 分钟。"})
	if err != nil || outcome.Status != "accepted" {
		return nil, errors.Join(denied("验证码邮件未确认提交成功"), err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.codes.entries[email] = CodeEntry{Hash: sha256.Sum256([]byte(code)), ExpiresAt: s.now().Add(15 * time.Minute)}
	return nil, nil
}

// 用途：承接原 POST /register 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) Register(ctx context.Context, in platform.Input) (any, error) {
	var fields map[string]json.RawMessage
	if err := decode(in, &fields); err != nil {
		return nil, err
	}
	for _, key := range []string{"roleIdList", "roleList", "roleId", "role", "roles", "userInfoId", "id"} {
		if _, ok := fields[key]; ok {
			return nil, invalid("注册不接受身份或角色参数")
		}
	}
	var p struct{ Username, Password, Code string }
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	email, err := normalizeEmail(p.Username)
	if err != nil {
		return nil, err
	}
	hash, err := HashPassword(p.Password)
	if err != nil {
		return nil, err
	}
	website, err := s.site.Config(ctx)
	if err != nil {
		return nil, err
	}
	avatar, _ := website["userAvatar"].(string)
	err = s.consumeCode(ctx, email, p.Code, false, func(tx *sql.Tx) (int64, error) {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tb_user_auth WHERE username=? COLLATE NOCASE)", email).Scan(&exists); err != nil {
			return 0, err
		}
		if exists {
			return 0, &platform.Error{Code: platform.CodeExists, Message: "邮箱已注册"}
		}
		var role int64
		if err := tx.QueryRowContext(ctx, "SELECT id FROM tb_role WHERE role_label='user' AND is_disable=0").Scan(&role); err != nil {
			return 0, fmt.Errorf("read default user role: %w", err)
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO tb_user_info(email,nickname,avatar,create_time) VALUES(?,'用户',?,?)", email, avatar, s.stamp())
		if err != nil {
			return 0, err
		}
		uid, err := result.LastInsertId()
		if err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE tb_user_info SET nickname=? WHERE id=?", fmt.Sprintf("用户%d", uid), uid); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO tb_user_auth(user_info_id,username,password,login_type,create_time) VALUES(?,?,?,1,?)", uid, email, hash, s.stamp()); err != nil {
			return 0, err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO tb_user_role(user_id,role_id) VALUES(?,?)", uid, role)
		return uid, err
	})
	return nil, err
}

// 用途：承接原 PUT /users/password 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdatePassword(ctx context.Context, in platform.Input) (any, error) {
	var p struct{ Username, Password, Code string }
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	email, err := normalizeEmail(p.Username)
	if err != nil {
		return nil, err
	}
	hash, err := HashPassword(p.Password)
	if err != nil {
		return nil, err
	}
	err = s.consumeCode(ctx, email, p.Code, true, func(tx *sql.Tx) (int64, error) {
		var id, uid int64
		err := tx.QueryRowContext(ctx, `SELECT a.id,a.user_info_id FROM tb_user_auth a JOIN tb_user_info i ON i.id=a.user_info_id WHERE a.username=? COLLATE NOCASE AND a.login_type=1 AND i.is_disable=0`, email).Scan(&id, &uid)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, missing("账号不存在或不可用")
		}
		if err != nil {
			return 0, err
		}
		_, err = tx.ExecContext(ctx, "UPDATE tb_user_auth SET password=?,update_time=? WHERE id=?", hash, s.stamp(), id)
		return uid, err
	})
	return nil, err
}

// 用途：承接原 PUT /admin/users/password 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateAdminPassword(ctx context.Context, in platform.Input) (any, error) {
	a, err := s.requireActor(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(ctx, a, "PUT", "/admin/users/password"); err != nil {
		return nil, err
	}
	var p struct{ OldPassword, NewPassword string }
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	hash, err := HashPassword(p.NewPassword)
	if err != nil {
		return nil, err
	}
	s.sessions.mu.Lock()
	defer s.sessions.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var old string
	var typ int
	if err := tx.QueryRowContext(ctx, "SELECT password,login_type FROM tb_user_auth WHERE id=? AND user_info_id=?", a.AuthID, a.UserInfoID).Scan(&old, &typ); err != nil {
		return nil, err
	}
	if typ != 1 || VerifyPassword(old, p.OldPassword) != nil {
		return nil, denied("旧密码不正确")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tb_user_auth SET password=?,update_time=? WHERE id=?", hash, s.stamp(), a.AuthID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.revokeUserLocked(a.UserInfoID)
	s.setTokenCookie(in, "", -1)
	return nil, nil
}

// 用途：承接原 PUT /users/info 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateUserInfo(ctx context.Context, in platform.Input) (any, error) {
	a, err := s.requireActor(ctx, in)
	if err != nil {
		return nil, err
	}
	var p struct {
		Nickname       string
		Intro, WebSite *string
	}
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Nickname) == "" {
		return nil, invalid("昵称不能为空")
	}
	if p.WebSite != nil && *p.WebSite != "" {
		u, e := url.Parse(*p.WebSite)
		if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return nil, invalid("个人网站地址无效")
		}
	}
	result, err := s.db.ExecContext(ctx, "UPDATE tb_user_info SET nickname=?,intro=COALESCE(?,intro),web_site=COALESCE(?,web_site),update_time=? WHERE id=? AND is_disable=0", p.Nickname, p.Intro, p.WebSite, s.stamp(), a.UserInfoID)
	return nil, changed(result, err)
}

func invalid(message string) error {
	return &platform.Error{Code: platform.CodeInvalid, Message: message}
}
func denied(message string) error { return &platform.Error{Code: platform.CodeFail, Message: message} }
func missing(message string) error {
	return &platform.Error{Code: platform.CodeMissing, Message: message}
}
func (s *Service) stamp() string { return s.now().In(s.cfg.TimeZone).Format("2006-01-02T15:04:05") }
func decode(in platform.Input, target any) error {
	data := in.Body
	if len(data) == 0 {
		m := make(map[string]string, len(in.Form))
		for key, values := range in.Form {
			if len(values) != 1 {
				return invalid("参数不能重复")
			}
			m[key] = values[0]
		}
		data, _ = json.Marshal(m)
	}
	if len(data) == 0 || data[0] != '{' || json.Unmarshal(data, target) != nil {
		return invalid("请求参数不正确")
	}
	return nil
}
func changed(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return missing("目标不存在或不可用")
	}
	return nil
}
func nullable(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}

func (s *Service) loginAccount(ctx context.Context, in platform.Input, id int64, plain *string, verified *social.VerifiedIdentity) (platform.Object, error) {
	if in.Response == nil {
		return nil, invalid("登录需要 HTTP 响应")
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	ipSource := ""
	if s.analytics != nil {
		ipSource = s.analytics.LookupIP(ctx, in.IP)
	}
	s.sessions.mu.Lock()
	defer s.sessions.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if verified != nil {
		id, err = s.socialAccountTx(ctx, tx, *verified)
		if err != nil {
			return nil, err
		}
	}
	var userID int64
	var encoded string
	var disabled, typ int
	err = tx.QueryRowContext(ctx, `SELECT a.user_info_id,a.password,a.login_type,i.is_disable FROM tb_user_auth a JOIN tb_user_info i ON i.id=a.user_info_id WHERE a.id=?`, id).Scan(&userID, &encoded, &typ, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, denied("账号或密码不正确")
	}
	if err != nil {
		return nil, err
	}
	if disabled != 0 {
		return nil, denied("账号已被禁用")
	}
	if plain != nil && (typ != 1 || VerifyPassword(encoded, *plain) != nil) {
		return nil, denied("账号或密码不正确")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE tb_user_auth SET ip_address=?,ip_source=?,last_login_time=?,update_time=? WHERE id=?", in.IP, ipSource, s.stamp(), s.stamp(), id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	detail, err := s.profile(ctx, id)
	if err != nil {
		return nil, err
	}
	sets, err := s.engagement.UserSets(ctx, userID)
	if err != nil {
		return nil, err
	}
	for key, value := range sets {
		detail[key] = value
	}
	detail["browser"], detail["os"] = in.Browser, in.OS
	s.pruneSessionsLocked()
	entry := Session{Token: token, AuthID: id, UserInfoID: userID, VisitorKey: in.VisitorKey, ExpiresAt: s.now().Add(10 * time.Hour)}
	s.sessions.byToken[token] = entry
	s.sessions.devices[token] = [2]string{in.Browser, in.OS}
	if in.VisitorKey != "" {
		s.sessions.byVisitor[in.VisitorKey] = token
	}
	s.setTokenCookie(in, token, 36000)
	return detail, nil
}

func (s *Service) socialLogin(ctx context.Context, in platform.Input, verified social.VerifiedIdentity) (any, error) {
	detail, err := s.loginAccount(ctx, in, 0, nil, &verified)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"roleList", "isDisable", "browser", "os"} {
		delete(detail, key)
	}
	return detail, nil
}
func (s *Service) socialAccountTx(ctx context.Context, tx *sql.Tx, verified social.VerifiedIdentity) (int64, error) {
	var id int64
	var typ int
	err := tx.QueryRowContext(ctx, "SELECT id,login_type FROM tb_user_auth WHERE username=? COLLATE NOCASE", verified.ExternalID).Scan(&id, &typ)
	if err == nil {
		if typ != verified.LoginType {
			return 0, &platform.Error{Code: platform.CodeExists, Message: "外部账号标识与已有账号冲突"}
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var role int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM tb_role WHERE role_label='user' AND is_disable=0").Scan(&role); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO tb_user_info(nickname,avatar,create_time) VALUES(?,?,?)", verified.Nickname, verified.Avatar, s.stamp())
	if err != nil {
		return 0, err
	}
	uid, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	// The NOT NULL legacy column remains empty for external identities. Provider
	// access tokens are transient verification inputs, never stored passwords.
	result, err = tx.ExecContext(ctx, "INSERT INTO tb_user_auth(user_info_id,username,password,login_type,create_time) VALUES(?,?,'',?,?)", uid, verified.ExternalID, verified.LoginType, s.stamp())
	if err != nil {
		return 0, err
	}
	id, err = result.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO tb_user_role(user_id,role_id) VALUES(?,?)", uid, role)
	return id, err
}
func (s *Service) profile(ctx context.Context, id int64) (platform.Object, error) {
	var auth, user int64
	var typ, disabled int
	var username, nickname, avatar string
	var email, intro, website, ip, source, last sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT a.id,a.user_info_id,a.login_type,a.username,i.email,i.nickname,i.avatar,i.intro,i.web_site,i.is_disable,a.ip_address,a.ip_source,a.last_login_time
		FROM tb_user_auth a JOIN tb_user_info i ON i.id=a.user_info_id WHERE a.id=?`, id).Scan(&auth, &user, &typ, &username, &email, &nickname, &avatar, &intro, &website, &disabled, &ip, &source, &last)
	if err != nil {
		return nil, fmt.Errorf("read user profile: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.role_label FROM tb_role r JOIN tb_user_role ur ON ur.role_id=r.id WHERE ur.user_id=? AND r.is_disable=0 ORDER BY r.id`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := []string{}
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, err
		}
		roles = append(roles, label)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return platform.Object{"id": auth, "userInfoId": user, "loginType": typ, "username": username, "email": nullable(email), "nickname": nickname, "avatar": avatar, "intro": nullable(intro), "webSite": nullable(website), "isDisable": disabled, "ipAddress": nullable(ip), "ipSource": nullable(source), "lastLoginTime": nullable(last), "roleList": roles}, nil
}

// 用途：承接原 POST /users/avatar 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateUserAvatar(ctx context.Context, in platform.Input) (any, error) {
	a, err := s.requireActor(ctx, in)
	if err != nil {
		return nil, err
	}
	if len(in.Files) != 1 || in.Files[0] == nil {
		return nil, invalid("请选择一个头像文件")
	}
	header := in.Files[0]
	body, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer body.Close()
	file, err := s.files.Put(ctx, "avatar", header.Filename, header.Header.Get("Content-Type"), body)
	if err != nil {
		return nil, err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE tb_user_info SET avatar=?,update_time=? WHERE id=? AND is_disable=0", file.URL, s.stamp(), a.UserInfoID)
	if err = changed(result, err); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return nil, errors.Join(err, s.files.Discard(cleanup, file))
	}
	return file.URL, nil
}

// 用途：承接原 POST /users/email 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveUserEmail(ctx context.Context, in platform.Input) (any, error) {
	a, err := s.requireActor(ctx, in)
	if err != nil {
		return nil, err
	}
	var p struct{ Email, Code string }
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	email, err := normalizeEmail(p.Email)
	if err != nil {
		return nil, err
	}
	err = s.consumeCode(ctx, email, p.Code, false, func(tx *sql.Tx) (int64, error) {
		result, err := tx.ExecContext(ctx, "UPDATE tb_user_info SET email=?,update_time=? WHERE id=? AND is_disable=0", email, s.stamp(), a.UserInfoID)
		return a.UserInfoID, changed(result, err)
	})
	return nil, err
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := stdmail.ParseAddress(value)
	if err != nil || address.Address != value || len(value) > 254 || strings.ContainsAny(value, "\r\n") {
		return "", invalid("邮箱格式不正确")
	}
	return value, nil
}
func (s *Service) consumeCode(ctx context.Context, email, code string, revoke bool, change func(*sql.Tx) (int64, error)) error {
	s.codes.mu.Lock()
	defer s.codes.mu.Unlock()
	entry, ok := s.codes.entries[email]
	digest := sha256.Sum256([]byte(code))
	if !ok || !s.now().Before(entry.ExpiresAt) || len(code) != 6 || subtle.ConstantTimeCompare(entry.Hash[:], digest[:]) != 1 {
		return invalid("验证码错误、过期或已使用")
	}
	s.sessions.mu.Lock()
	defer s.sessions.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	uid, err := change(tx)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	delete(s.codes.entries, email)
	if revoke {
		s.revokeUserLocked(uid)
	}
	return nil
}

// 用途：将本模块 schema.sql 交给唯一迁移器，业务逻辑不自动建表。
//
//go:embed schema.sql
var Schema string

// Public protocol projections use platform.Object; no database row is serialized directly.
// Integer/Long: JSON number; Date: Unix milliseconds; LocalDateTime: yyyy-MM-ddTHH:mm:ss.
// Optional fields retain null; lists/sets are arrays. UserDetailDTO never includes password.
// UserDetailDTO: id:Integer, userInfoId:Integer, email:String, loginType:Integer, username:String, roleList:String[], nickname:String, avatar:String, intro:String, webSite:String, articleLikeSet:Object[], commentLikeSet:Object[], talkLikeSet:Object[], ipAddress:String, ipSource:String, isDisable:Integer, browser:String, os:String, lastLoginTime:LocalDateTime
// UserInfoDTO: id:Integer, userInfoId:Integer, email:String, loginType:Integer, username:String, nickname:String, avatar:String, intro:String, webSite:String, articleLikeSet:Object[], commentLikeSet:Object[], talkLikeSet:Object[], ipAddress:String, ipSource:String, lastLoginTime:LocalDateTime
// UserBackDTO: id:Integer, userInfoId:Integer, avatar:String, nickname:String, roleList:UserRoleDTO[], loginType:Integer, ipAddress:String, ipSource:String, createTime:Date, lastLoginTime:Date, isDisable:Integer, status:Integer
// UserOnlineDTO: userInfoId:Integer, nickname:String, avatar:String, ipAddress:String, ipSource:String, browser:String, os:String, lastLoginTime:LocalDateTime
// UserRoleDTO: id:Integer, roleName:String
// RoleDTO: id:Integer, roleName:String, roleLabel:String, createTime:LocalDateTime, isDisable:Integer, resourceIdList:Integer[], menuIdList:Integer[]
// MenuDTO: id:Integer, name:String, path:String, component:String, icon:String, createTime:LocalDateTime, orderNum:Integer, isDisable:Integer, isHidden:Integer, children:MenuDTO[]
// UserMenuDTO: name:String, path:String, component:String, icon:String, hidden:Boolean, children:UserMenuDTO[]
// ResourceDTO: id:Integer, resourceName:String, url:String, requestMethod:String, isDisable:Integer, isAnonymous:Integer, createTime:LocalDateTime, children:ResourceDTO[]
// LabelOptionDTO: id:Integer, label:String, children:LabelOptionDTO[]
// UserVO: username:String, password:String, code:String
// UserInfoVO: nickname:String, intro:String, webSite:String
// UserRoleVO: userInfoId:Integer, nickname:String, roleIdList:Integer[]
// UserDisableVO: id:Integer, isDisable:Integer
// RoleVO: id:Integer, roleName:String, roleLabel:String, resourceIdList:Integer[], menuIdList:Integer[]
// MenuVO: id:Integer, name:String, icon:String, path:String, component:String, orderNum:Integer, parentId:Integer, isHidden:Integer
// ResourceVO: id:Integer, resourceName:String, url:String, requestMethod:String, parentId:Integer, isAnonymous:Integer
// PasswordVO: oldPassword:String, newPassword:String
// EmailVO: email:String, code:String
// QQLoginVO: openId:String, accessToken:String
// WeiboLoginVO: code:String
