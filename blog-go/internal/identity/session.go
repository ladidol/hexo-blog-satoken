package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"blog-go/internal/platform"
)

// 用途：会话仅保存当前身份引用与到期事实，不作为持久用户资料副本。
type Session struct {
	Token      string
	AuthID     int64
	UserInfoID int64
	VisitorKey string
	ExpiresAt  time.Time
}

// 用途：并发保护进程内会话，并支持当前浏览器已打开聊天的身份刷新。
type SessionStore struct {
	mu        sync.RWMutex
	byToken   map[string]Session
	byVisitor map[string]string
	devices   map[string][2]string
}

// 用途：在每次 HTTP 请求恢复身份；失效/禁用时拒绝。
func (s *Service) Resolve(r *http.Request) (platform.Actor, error) {
	visitor := ""
	if cookie, err := r.Cookie(platform.VisitorCookie); err == nil {
		visitor = cookie.Value
	}
	return s.resolveToken(r.Context(), requestToken(r), visitor)
}

// 用途：为既有 WebSocket 按游客浏览会话取得当前有效登录身份。
func (s *Service) ActorForVisitor(ctx context.Context, visitorKey string) (platform.Actor, error) {
	s.sessions.mu.RLock()
	token := s.sessions.byVisitor[visitorKey]
	s.sessions.mu.RUnlock()
	return s.resolveToken(ctx, token, visitorKey)
}

func requestToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if cookie, err := r.Cookie(platform.TokenCookie); err == nil {
		return cookie.Value
	}
	return ""
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *Service) removeTokenLocked(token string) {
	entry, ok := s.sessions.byToken[token]
	if !ok {
		return
	}
	delete(s.sessions.byToken, token)
	delete(s.sessions.devices, token)
	if s.sessions.byVisitor[entry.VisitorKey] == token {
		delete(s.sessions.byVisitor, entry.VisitorKey)
	}
}
func (s *Service) revokeUserLocked(id int64) {
	for token, entry := range s.sessions.byToken {
		if entry.UserInfoID == id {
			s.removeTokenLocked(token)
		}
	}
}
func (s *Service) pruneSessionsLocked() {
	for token, entry := range s.sessions.byToken {
		if !s.now().Before(entry.ExpiresAt) {
			s.removeTokenLocked(token)
		}
	}
}
func (s *Service) resolveToken(ctx context.Context, token, visitor string) (platform.Actor, error) {
	guest := platform.Actor{VisitorKey: visitor}
	if err := ctx.Err(); err != nil {
		return guest, err
	}
	s.sessions.mu.Lock()
	entry, ok := s.sessions.byToken[token]
	if ok && !s.now().Before(entry.ExpiresAt) {
		s.removeTokenLocked(token)
		ok = false
	}
	s.sessions.mu.Unlock()
	if !ok {
		return guest, nil
	}
	actor, err := s.activeActor(ctx, entry.AuthID, entry.UserInfoID)
	if err != nil {
		var business *platform.Error
		if errors.As(err, &business) {
			s.sessions.mu.Lock()
			s.revokeUserLocked(entry.UserInfoID)
			s.sessions.mu.Unlock()
			return guest, nil
		}
		return guest, err
	}
	s.sessions.mu.Lock()
	defer s.sessions.mu.Unlock()
	// Another request may have moved this token to a different visitor while
	// the database read was in flight. Remove the current association, not the
	// earlier snapshot, so concurrent refreshes cannot retain stale bindings.
	entry, ok = s.sessions.byToken[token]
	if !ok || !s.now().Before(entry.ExpiresAt) {
		s.removeTokenLocked(token)
		return guest, nil
	}
	// A valid login token can refresh the browser-to-login association. A
	// visitor cookie by itself never reaches this branch with an identity.
	if visitor != "" && entry.VisitorKey != visitor {
		if s.sessions.byVisitor[entry.VisitorKey] == token {
			delete(s.sessions.byVisitor, entry.VisitorKey)
		}
		entry.VisitorKey = visitor
		s.sessions.byToken[token] = entry
		s.sessions.byVisitor[visitor] = token
	}
	actor.VisitorKey = visitor
	return actor, nil
}
func (s *Service) activeActor(ctx context.Context, authID, userID int64) (platform.Actor, error) {
	if authID <= 0 || userID <= 0 {
		return platform.Actor{}, denied("请先登录")
	}
	var nickname, avatar string
	var disabled int
	err := s.db.QueryRowContext(ctx, `SELECT i.nickname,i.avatar,i.is_disable FROM tb_user_auth a JOIN tb_user_info i ON i.id=a.user_info_id WHERE a.id=? AND i.id=?`, authID, userID).Scan(&nickname, &avatar, &disabled)
	if errors.Is(err, sql.ErrNoRows) || err == nil && disabled != 0 {
		return platform.Actor{}, denied("账号不存在或已禁用")
	}
	if err != nil {
		return platform.Actor{}, err
	}
	return platform.Actor{AuthID: authID, UserInfoID: userID, Nickname: nickname, Avatar: avatar, LoggedIn: true}, nil
}
func (s *Service) requireActor(ctx context.Context, in platform.Input) (platform.Actor, error) {
	if !in.Actor.LoggedIn {
		return platform.Actor{}, denied("请先登录")
	}
	if in.Request != nil {
		a, err := s.Resolve(in.Request)
		if err != nil {
			return a, err
		}
		if !a.LoggedIn {
			return a, denied("请先登录")
		}
		return a, nil
	}
	return s.activeActor(ctx, in.Actor.AuthID, in.Actor.UserInfoID)
}
func (s *Service) setTokenCookie(in platform.Input, token string, age int) {
	if in.Response == nil {
		return
	}
	cookie := &http.Cookie{Name: platform.TokenCookie, Value: token, Path: "/", HttpOnly: true, MaxAge: age, SameSite: http.SameSiteLaxMode, Secure: strings.HasPrefix(s.cfg.PublicURL, "https://") || (in.Request != nil && in.Request.TLS != nil)}
	if age < 0 {
		cookie.Expires = time.Unix(1, 0)
	}
	http.SetCookie(in.Response, cookie)
}
