package chat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"blog-go/internal/platform"
	"github.com/gorilla/websocket"
)

// 用途：验证 origin/建立访客归属并升级 WebSocket，发送12小时历史。
func (s *Service) ServeWebSocket(w http.ResponseWriter, r *http.Request) {
	if !s.beginWork() {
		http.Error(w, "chat unavailable", http.StatusServiceUnavailable)
		return
	}
	defer s.workers.Done()
	if !s.originAllowed(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	visitor, err := visitorCookie(w, r, s.cfg)
	if err != nil {
		http.Error(w, "visitor session unavailable", http.StatusInternalServerError)
		return
	}
	upgrader := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, HandshakeTimeout: 5 * time.Second, CheckOrigin: s.originAllowed}
	conn, err := upgrader.Upgrade(w, r, w.Header().Clone())
	if err != nil {
		return
	}
	defer conn.Close()
	client := &Client{Conn: conn, VisitorKey: visitor, Send: make(chan Frame, 64), ipAddress: remoteIP(r.RemoteAddr)}
	if s.analytics != nil {
		client.ipSource = s.analytics.LookupIP(s.ctx, client.ipAddress)
	}
	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(90 * time.Second)) })
	err = s.execute(r.Context(), func(ctx context.Context) error {
		history, err := s.history(ctx)
		if err != nil {
			return err
		}
		client.Send <- Frame{Type: 2, Data: platform.Object{"chatRecordList": history, "ipAddress": client.ipAddress, "ipSource": client.ipSource}}
		s.hub.register(client)
		return nil
	})
	if err != nil {
		closeWithError(conn, err)
		return
	}
	written := make(chan struct{})
	go writeClient(client, written)
	defer func() {
		_ = conn.Close()
		select {
		case s.hub.Unregister <- client:
		case <-s.done:
		}
		<-written
	}()
	for {
		typ, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		data = bytes.TrimSpace(data)
		if typ != websocket.TextMessage || !utf8.Valid(data) || len(data) == 0 || data[0] != '{' {
			closeWithError(conn, malformed())
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var frame Frame
		if decoder.Decode(&frame) != nil {
			closeWithError(conn, malformed())
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			closeWithError(conn, malformed())
			return
		}
		if err := s.HandleFrame(r.Context(), client, frame); err != nil {
			closeWithError(conn, err)
			return
		}
		if err := conn.SetReadDeadline(time.Now().Add(90 * time.Second)); err != nil {
			return
		}
	}
}

// 用途：将下列原路径逐项绑定本模块真实业务方法。
func (s *Service) Routes() []platform.Route {
	return []platform.Route{{Spec: RouteSpecs[0], Handle: s.SendVoice}}
}

func writeClient(client *Client, done chan struct{}) {
	defer close(done)
	defer client.Conn.Close()
	for frame := range client.Send {
		if err := client.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return
		}
		if err := client.Conn.WriteJSON(frame); err != nil {
			return
		}
	}
}
func closeWithError(conn *websocket.Conn, err error) {
	code, message := websocket.CloseInternalServerErr, "chat operation failed"
	var protocol *protocolError
	if errors.As(err, &protocol) {
		code, message = protocol.code, protocol.message
	} else if errors.Is(err, context.Canceled) {
		code, message = websocket.CloseGoingAway, "chat closing"
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, message), time.Now().Add(time.Second))
}
func remoteIP(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}
func (s *Service) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range s.cfg.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	scheme := "http://"
	if r.TLS != nil {
		scheme = "https://"
	}
	return origin == scheme+r.Host
}
func visitorCookie(w http.ResponseWriter, r *http.Request, cfg platform.Config) (string, error) {
	if cookie, err := r.Cookie(platform.VisitorCookie); err == nil {
		if raw, err := base64.RawURLEncoding.DecodeString(cookie.Value); err == nil && len(raw) == 32 {
			return cookie.Value, nil
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	http.SetCookie(w, &http.Cookie{Name: platform.VisitorCookie, Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || strings.HasPrefix(cfg.PublicURL, "https://")})
	return value, nil
}

// 用途：作为聊天室连接与消息所有权路由、权限和行为追溯的唯一注册清单。
var RouteSpecs = []platform.RouteSpec{
	{Method: "POST", Path: "/voice", Operation: "SendVoice", Policy: "public", Audit: false},
}

// P-121 HTTP POST /voice

// P-122 WebSocket WebSocket GET /websocket
// 约束变化：原type/data 1..6结构沿用；增加服务端归属、慢连接隔离和一次编码pong；无效载荷不写库，策略拒绝1008/坏数据1007/超大1009；实际双连接与12小时历史验收。 具体字段与状态解释见MODULE-012已确认执行边界。
