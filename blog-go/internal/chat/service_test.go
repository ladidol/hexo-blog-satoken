package chat_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blog-go/internal/analytics"
	"blog-go/internal/article"
	"blog-go/internal/bootstrap"
	"blog-go/internal/catalog"
	"blog-go/internal/chat"
	"blog-go/internal/community"
	"blog-go/internal/engagement"
	"blog-go/internal/files"
	"blog-go/internal/identity"
	"blog-go/internal/mail"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
	"blog-go/internal/social"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

var ctx = context.Background()
var now = time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)

type fixture struct {
	s        *chat.Service
	db       *sql.DB
	cfg      platform.Config
	server   *httptest.Server
	identity *identity.Service
	site     *site.Service
}

func setup(t *testing.T) *fixture {
	t.Helper()
	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	f := &fixture{server: server, cfg: platform.Config{DataDir: t.TempDir(), PublicURL: "http://" + server.Listener.Addr().String(), FrontendURL: "http://frontend.test", AllowedOrigins: []string{"http://frontend.test"}, TimeZone: time.FixedZone("Asia/Shanghai", 8*3600), Values: map[string]string{}}}
	var err error
	f.db, err = platform.OpenSQLite(f.cfg)
	must(t, err)
	t.Cleanup(func() { _ = f.db.Close() })
	must(t, platform.Migrate(ctx, f.db, []string{identity.Schema, catalog.Schema, article.Schema, community.Schema, media.Schema, site.Schema, analytics.Schema, mail.Schema, chat.Schema, engagement.Schema}, func(ctx context.Context, tx *sql.Tx) error { return bootstrap.Seed(ctx, tx, f.cfg) }))
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), 10)
	must(t, err)
	execute(t, f.db, `INSERT INTO tb_user_info(id,nickname,avatar,create_time) VALUES(11,'Actual user','https://example.test/actual.png','2026-09-12T16:00:00');
		INSERT INTO tb_user_auth(id,user_info_id,username,password,login_type,create_time) VALUES(101,11,'chat-user','',1,'2026-09-12T16:00:00');
		INSERT INTO tb_user_role(user_id,role_id) VALUES(11,2)`)
	execute(t, f.db, "UPDATE tb_user_auth SET password=? WHERE id=101", string(hash))
	clock := func() time.Time { return now }
	fileService := files.New(f.db, f.cfg, nil, clock)
	stats := analytics.New(f.db, f.cfg, nil, clock)
	f.site = site.New(f.db, f.cfg, clock)
	f.identity = identity.New(f.db, f.cfg, fileService, mail.New(f.db, f.cfg, nil, clock), social.New(f.db, f.cfg, nil, clock), f.site, stats, engagement.New(f.db, f.cfg, clock), clock)
	f.s = chat.New(f.db, f.cfg, fileService, stats, f.identity.ActorForVisitor, clock)
	t.Cleanup(func() {
		timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := f.s.Close(timeout); err != nil {
			t.Error(err)
		}
	})
	routes := append(f.s.Routes(), f.identity.Routes()...)
	routes = append(routes, platform.Route{Spec: platform.RouteSpec{Method: "GET", Path: "/websocket", Operation: "ServeWebSocket", Policy: "public"}, Raw: http.HandlerFunc(f.s.ServeWebSocket)}, platform.Route{Spec: platform.RouteSpec{Method: "GET", Path: "/files/{path...}", Operation: "ServeFile", Policy: "public"}, Raw: http.HandlerFunc(fileService.ServeFile)})
	engine, err := platform.NewRouter(routes, platform.Callbacks{Resolve: f.identity.Resolve, Authorize: f.identity.Authorize}, f.cfg)
	must(t, err)
	server.Config.Handler = engine
	server.Start()
	return f
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func execute(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	_, err := db.Exec(q, args...)
	must(t, err)
}
func count(t *testing.T, f *fixture, q string, args ...any) int64 {
	t.Helper()
	var n int64
	must(t, f.db.QueryRow(q, args...).Scan(&n))
	return n
}
func stamp(t time.Time) string {
	return t.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02T15:04:05")
}
func read(t *testing.T, c *websocket.Conn) chat.Frame {
	t.Helper()
	must(t, c.SetReadDeadline(time.Now().Add(4*time.Second)))
	var frame chat.Frame
	must(t, c.ReadJSON(&frame))
	return frame
}
func until(t *testing.T, c *websocket.Conn, typ int) chat.Frame {
	t.Helper()
	for i := 0; i < 100; i++ {
		frame := read(t, c)
		if frame.Type == typ {
			return frame
		}
	}
	t.Fatalf("frame %d not received", typ)
	return chat.Frame{}
}
func object(t *testing.T, frame chat.Frame) map[string]any {
	t.Helper()
	data, ok := frame.Data.(map[string]any)
	if !ok {
		t.Fatalf("frame data is not an object: %#v", frame)
	}
	return data
}
func dial(t *testing.T, f *fixture, cookie *http.Cookie) (*websocket.Conn, *http.Cookie, chat.Frame) {
	t.Helper()
	website, err := f.site.Config(ctx)
	must(t, err)
	headers := http.Header{"Origin": {f.cfg.FrontendURL}}
	if cookie != nil {
		headers.Set("Cookie", cookie.String())
	}
	c, response, err := websocket.DefaultDialer.Dial(website["websocketUrl"].(string), headers)
	must(t, err)
	t.Cleanup(func() { _ = c.Close() })
	defer response.Body.Close()
	if cookie == nil {
		for _, candidate := range response.Cookies() {
			if candidate.Name == platform.VisitorCookie {
				cookie = candidate
			}
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.MaxAge != 0 {
		t.Fatalf("missing browser session cookie: %#v", cookie)
	}
	value, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(value) != 32 {
		t.Fatal("visitor identity is not random 32-byte cookie")
	}
	history := until(t, c, 2)
	until(t, c, 1)
	return c, cookie, history
}
func textFrame(content string) chat.Frame {
	return chat.Frame{Type: 3, Data: map[string]any{"nickname": "<b>Guest</b>", "avatar": "https://example.test/guest.png", "content": content, "userId": 999, "ipAddress": "forged-ip", "ipSource": "forged-region", "createTime": "1900-01-01T00:00:00"}}
}
func wav(sample byte) []byte {
	data := make([]byte, 48)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], 40)
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 4)
	data[44] = sample
	return data
}
func voice(t *testing.T, f *fixture, cookie *http.Cookie, data []byte, avatars ...string) platform.Result {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="voice.wav"`}, "Content-Type": {"audio/wav"}})
	must(t, err)
	_, err = part.Write(data)
	must(t, err)
	avatar := "https://example.test/guest.png"
	if len(avatars) > 0 {
		avatar = avatars[0]
	}
	for k, v := range map[string]string{"type": "5", "nickname": "Guest voice", "avatar": avatar, "userId": "999", "ipAddress": "forged-ip", "ipSource": "forged-region"} {
		must(t, form.WriteField(k, v))
	}
	must(t, form.Close())
	r, err := http.NewRequest("POST", f.server.URL+"/voice", &body)
	must(t, err)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.AddCookie(cookie)
	response, err := f.server.Client().Do(r)
	must(t, err)
	defer response.Body.Close()
	var result platform.Result
	must(t, json.NewDecoder(response.Body).Decode(&result))
	return result
}
func login(t *testing.T, f *fixture, cookie *http.Cookie) *http.Cookie {
	t.Helper()
	r, err := http.NewRequest("POST", f.server.URL+"/login", strings.NewReader(url.Values{"username": {"chat-user"}, "password": {"correct-password"}}.Encode()))
	must(t, err)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(cookie)
	response, err := f.server.Client().Do(r)
	must(t, err)
	defer response.Body.Close()
	var result platform.Result
	must(t, json.NewDecoder(response.Body).Decode(&result))
	if !result.Flag {
		t.Fatalf("login failed: %v", result)
	}
	for _, token := range response.Cookies() {
		if token.Name == platform.TokenCookie {
			return token
		}
	}
	t.Fatal("missing login cookie")
	return nil
}

func TestChatTextVoiceRecallAndHistoryOverRealWebSockets(t *testing.T) {
	f := setup(t)
	for _, at := range []time.Time{now.Add(-13 * time.Hour), now.Add(-time.Hour)} {
		execute(t, f.db, "INSERT INTO tb_chat_record(nickname,avatar,content,ip_address,ip_source,type,create_time,owner_key) VALUES('history','/history.png','past','192.0.2.1','',3,?,'g:past')", stamp(at))
	}
	a, cookieA, history := dial(t, f, nil)
	if len(object(t, history)["chatRecordList"].([]any)) != 1 {
		t.Fatal("history is not the last 12 hours")
	}
	if count(t, f, "SELECT count(*) FROM tb_chat_record") != 2 {
		t.Fatal("startup removed old business records")
	}
	b, _, _ := dial(t, f, nil)
	if frame := until(t, a, 1); frame.Data != float64(2) {
		t.Fatal("online count is not real connection count")
	}
	must(t, a.WriteJSON(textFrame(`<script>bad()</script><b>Hello</b>`)))
	first, second := object(t, until(t, a, 3)), object(t, until(t, b, 3))
	if first["id"] != second["id"] || first["userId"] != nil || first["nickname"] != "Guest" || first["ipAddress"] != "127.0.0.1" || first["ipSource"] != "" || first["content"] != "<b>Hello</b>" {
		t.Fatalf("untrusted text projection: %v", first)
	}
	if first["createTime"] != stamp(now) || first["updateTime"] != nil || first["owner_key"] != nil {
		t.Fatal("incorrect public record projection")
	}
	digest := sha256.Sum256([]byte(cookieA.Value))
	var owner string
	must(t, f.db.QueryRow("SELECT owner_key FROM tb_chat_record WHERE id=?", first["id"]).Scan(&owner))
	if owner != "g:"+hex.EncodeToString(digest[:]) {
		t.Fatal("guest ownership is not tied to browser cookie")
	}
	must(t, a.WriteJSON(chat.Frame{Type: 6, Data: "ping"}))
	if pong := until(t, a, 6); pong.Data != "pong" {
		t.Fatal("heartbeat was encoded more than once")
	}
	result := voice(t, f, cookieA, wav(1))
	if !result.Flag || result.Data != nil {
		t.Fatalf("voice HTTP response=%#v", result)
	}
	one, two := object(t, until(t, a, 5)), object(t, until(t, b, 5))
	if one["id"] != two["id"] || one["userId"] != nil || one["ipAddress"] != "127.0.0.1" {
		t.Fatalf("voice broadcast=%v", one)
	}
	response, err := http.Get(one["content"].(string))
	must(t, err)
	payload, err := io.ReadAll(response.Body)
	response.Body.Close()
	must(t, err)
	if response.StatusCode != 200 || !bytes.Equal(payload, wav(1)) {
		t.Fatal("broadcast voice cannot be played/read back")
	}
	must(t, a.WriteJSON(chat.Frame{Type: 4, Data: map[string]any{"id": one["id"], "isVoice": false}}))
	recall := object(t, until(t, a, 4))
	until(t, b, 4)
	if recall["isVoice"] != true || count(t, f, "SELECT count(*) FROM tb_chat_record WHERE id=?", one["id"]) != 0 {
		t.Fatal("voice recall trusted payload or failed persistence")
	}
	must(t, b.Close())
	if online := until(t, a, 1); online.Data != float64(1) {
		t.Fatal("disconnect did not update count")
	}
	_, _, history = dial(t, f, cookieA)
	rows := object(t, history)["chatRecordList"].([]any)
	if len(rows) != 2 {
		t.Fatalf("reconnect history has old/recalled records: %v", rows)
	}
	if count(t, f, "SELECT count(*) FROM tb_chat_record") != 3 {
		t.Fatal("history filtering deleted durable records")
	}
}

func TestGuestRecallCannotUseAnotherVisitorWithSameIP(t *testing.T) {
	f := setup(t)
	a, _, _ := dial(t, f, nil)
	b, _, _ := dial(t, f, nil)
	until(t, a, 1)
	must(t, a.WriteJSON(textFrame("owned by A")))
	message := object(t, until(t, a, 3))
	until(t, b, 3)
	must(t, b.WriteJSON(chat.Frame{Type: 4, Data: map[string]any{"id": message["id"], "isVoice": true}}))
	_, _, err := b.ReadMessage()
	if !websocket.IsCloseError(err, 1008) {
		t.Fatalf("cross-visitor recall close=%v", err)
	}
	if count(t, f, "SELECT count(*) FROM tb_chat_record WHERE id=?", message["id"]) != 1 {
		t.Fatal("same-IP visitor deleted another guest's message")
	}
	if online := until(t, a, 1); online.Data != float64(1) {
		t.Fatal("rejected client still online")
	}
	must(t, a.WriteJSON(chat.Frame{Type: 4, Data: map[string]any{"id": message["id"]}}))
	if recall := object(t, until(t, a, 4)); recall["isVoice"] != false {
		t.Fatal("text recall type was not loaded from database")
	}
}

func TestExistingChatConnectionRefreshesLoginLogoutAndProfile(t *testing.T) {
	f := setup(t)
	a, cookie, _ := dial(t, f, nil)
	token := login(t, f, cookie)
	b, otherCookie, _ := dial(t, f, nil)
	login(t, f, otherCookie)
	if online := until(t, a, 1); online.Data != float64(2) {
		t.Fatal("two connections of the same user were deduplicated")
	}
	must(t, a.WriteJSON(textFrame("logged in")))
	record := object(t, until(t, a, 3))
	until(t, b, 3)
	if record["userId"] != float64(11) || record["nickname"] != "Actual user" || record["avatar"] != "https://example.test/actual.png" {
		t.Fatalf("login actor not refreshed: %v", record)
	}
	must(t, b.WriteJSON(textFrame("same user, other browser")))
	other := object(t, until(t, a, 3))
	until(t, b, 3)
	must(t, a.WriteJSON(chat.Frame{Type: 4, Data: map[string]any{"id": other["id"]}}))
	until(t, a, 4)
	until(t, b, 4)
	if count(t, f, "SELECT count(*) FROM tb_chat_record WHERE id=?", other["id"]) != 0 {
		t.Fatal("user ownership did not span browser sessions")
	}
	execute(t, f.db, "UPDATE tb_user_info SET nickname='Updated actor' WHERE id=11")
	must(t, a.WriteJSON(textFrame("profile changed")))
	if next := object(t, until(t, a, 3)); next["nickname"] != "Updated actor" {
		t.Fatal("cached nickname used")
	}
	r, err := http.NewRequest("POST", f.server.URL+"/logout", nil)
	must(t, err)
	r.AddCookie(cookie)
	r.AddCookie(token)
	response, err := f.server.Client().Do(r)
	must(t, err)
	response.Body.Close()
	must(t, a.WriteJSON(textFrame("after logout")))
	if next := object(t, until(t, a, 3)); next["userId"] != nil || next["nickname"] != "Guest" {
		t.Fatal("logged-out WebSocket retained login identity")
	}
	must(t, a.WriteJSON(chat.Frame{Type: 4, Data: map[string]any{"id": record["id"]}}))
	_, _, err = a.ReadMessage()
	if !websocket.IsCloseError(err, 1008) {
		t.Fatalf("guest recalled prior login-owned record: %v", err)
	}
}

func TestHubSlowConsumerIsolationAndCancellation(t *testing.T) {
	h := &chat.Hub{Register: make(chan *chat.Client), Unregister: make(chan *chat.Client), Broadcast: make(chan chat.Frame), Clients: make(map[*chat.Client]struct{})}
	running, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Run(running) }()
	fast := &chat.Client{Send: make(chan chat.Frame, 64)}
	slow := &chat.Client{Send: make(chan chat.Frame, 64)}
	h.Register <- fast
	receive := func() chat.Frame {
		select {
		case frame := <-fast.Send:
			return frame
		case <-time.After(3 * time.Second):
			t.Fatal("healthy client blocked behind slow peer")
			return chat.Frame{}
		}
	}
	if receive().Data != 1 {
		t.Fatal("first online count")
	}
	h.Register <- slow
	if receive().Data != 2 {
		t.Fatal("second online count")
	}
	for i := 0; i < 64; i++ {
		h.Broadcast <- chat.Frame{Type: 3, Data: i}
		if frame := receive(); frame.Type != 3 || frame.Data != i {
			t.Fatalf("broadcast order changed: %#v", frame)
		}
	}
	if frame := receive(); frame.Type != 1 || frame.Data != 1 {
		t.Fatal("full slow queue not disconnected")
	}
	for range slow.Send {
	}
	cancel()
	select {
	case err := <-done:
		must(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("hub cancellation blocked")
	}
	if len(h.Clients) != 0 {
		t.Fatal("hub retained connections after shutdown")
	}
}

func TestChatRejectsMalformedPolicyAndOversizedFrames(t *testing.T) {
	f := setup(t)
	for _, test := range []struct {
		name    string
		payload []byte
		code    int
	}{
		{"json", []byte(`{"type":`), 1007},
		{"utf8", []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}, 1007},
		{"shape", []byte(`{"type":3,"data":[]}`), 1007},
		{"server-only-type", []byte(`{"type":5,"data":{}}`), 1008},
		{"fractional-recall", []byte(`{"type":4,"data":{"id":1.5}}`), 1007},
		{"empty-content", []byte(`{"type":3,"data":{"content":"<script>bad()</script>","nickname":"Guest","avatar":"/guest.png"}}`), 1008},
		{"unsafe-avatar", []byte(`{"type":3,"data":{"content":"hello","nickname":"Guest","avatar":"javascript:bad()"}}`), 1008},
		{"oversized", []byte(`{"type":3,"data":{"content":"` + strings.Repeat("a", 1<<20) + `"}}`), 1009},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _, _ := dial(t, f, nil)
			must(t, c.WriteMessage(websocket.TextMessage, test.payload))
			_, _, err := c.ReadMessage()
			if !websocket.IsCloseError(err, test.code) {
				t.Fatalf("close=%v want %d", err, test.code)
			}
			if count(t, f, "SELECT count(*) FROM tb_chat_record") != 0 {
				t.Fatal("invalid frame persisted a record")
			}
		})
	}
	c, _, _ := dial(t, f, nil)
	must(t, c.WriteJSON(textFrame(strings.Repeat("界", 1001))))
	_, _, err := c.ReadMessage()
	if !websocket.IsCloseError(err, 1008) {
		t.Fatalf("long content close=%v", err)
	}
}

func TestChatWriteFailuresNeverBroadcastAndCompensateVoice(t *testing.T) {
	f := setup(t)
	a, cookie, _ := dial(t, f, nil)
	b, _, _ := dial(t, f, nil)
	until(t, a, 1)
	execute(t, f.db, `CREATE TRIGGER reject_chat BEFORE INSERT ON tb_chat_record BEGIN SELECT RAISE(ABORT,'chat rejected'); END`)
	result := voice(t, f, cookie, wav(2))
	if result.Flag {
		t.Fatal("failed chat persistence returned voice success")
	}
	objects, err := filepath.Glob(filepath.Join(f.cfg.DataDir, "files", "voice", "*.wav"))
	must(t, err)
	if len(objects) != 0 {
		t.Fatal("failed voice left an unreferenced file")
	}
	must(t, a.WriteJSON(chat.Frame{Type: 6, Data: "ping"}))
	if frame := read(t, a); frame.Type != 6 {
		t.Fatalf("failed voice broadcast frame %#v", frame)
	}
	must(t, a.WriteJSON(textFrame("must not broadcast")))
	_, _, err = a.ReadMessage()
	if !websocket.IsCloseError(err, 1011) {
		t.Fatalf("database failure close=%v", err)
	}
	if frame := read(t, b); frame.Type != 1 || frame.Data != float64(1) {
		t.Fatalf("failed text broadcast a record: %#v", frame)
	}
	if count(t, f, "SELECT count(*) FROM tb_chat_record") != 0 {
		t.Fatal("failed write persisted partial message")
	}
	execute(t, f.db, "DROP TRIGGER reject_chat")
	result = voice(t, f, cookie, []byte("not wav"))
	if result.Flag {
		t.Fatal("invalid audio upload succeeded")
	}
	must(t, b.WriteJSON(chat.Frame{Type: 6, Data: "ping"}))
	if frame := read(t, b); frame.Type != 6 {
		t.Fatal("invalid upload broadcast a voice")
	}
}

func TestConcurrentRegistrationAndBroadcastHaveNoHistoryGapOrDuplicates(t *testing.T) {
	f := setup(t)
	a, _, _ := dial(t, f, nil)
	writes := make(chan error, 1)
	go func() {
		for i := 0; i < 30; i++ {
			if err := a.WriteJSON(textFrame(fmt.Sprintf("message-%02d", i))); err != nil {
				writes <- err
				return
			}
		}
		writes <- a.WriteJSON(chat.Frame{Type: 6, Data: "barrier"})
	}()
	b, _, history := dial(t, f, nil)
	must(t, <-writes)
	until(t, a, 6)
	seen := map[int64]bool{}
	last := int64(0)
	add := func(record map[string]any) {
		id := int64(record["id"].(float64))
		if seen[id] || id <= last {
			t.Fatalf("duplicate or out-of-order record %d after %d", id, last)
		}
		seen[id] = true
		last = id
	}
	for _, record := range object(t, history)["chatRecordList"].([]any) {
		add(record.(map[string]any))
	}
	for len(seen) < 30 {
		frame := read(t, b)
		if frame.Type == 3 {
			add(object(t, frame))
		}
	}
	if count(t, f, "SELECT count(*) FROM tb_chat_record") != 30 {
		t.Fatal("message count differs from delivered union")
	}
}

func TestChatOriginRejectionAndShutdownReleaseConnections(t *testing.T) {
	f := setup(t)
	_, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.server.URL, "http")+"/websocket", http.Header{"Origin": {"https://evil.example"}})
	if err == nil || response == nil || response.StatusCode != 403 {
		t.Fatalf("cross-origin websocket allowed: %v", err)
	}
	response.Body.Close()
	a, _, _ := dial(t, f, nil)
	b, _, _ := dial(t, f, nil)
	until(t, a, 1)
	timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	must(t, f.s.Close(timeout))
	must(t, f.s.Close(timeout))
	for _, conn := range []*websocket.Conn{a, b} {
		must(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
		_, _, err := conn.ReadMessage()
		if err == nil {
			t.Fatal("service shutdown left socket open")
		}
	}
	r, err := http.Get(f.server.URL + "/websocket")
	must(t, err)
	defer r.Body.Close()
	if r.StatusCode != 503 {
		t.Fatalf("closed service accepted new socket: %d", r.StatusCode)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := f.s.HandleFrame(canceled, &chat.Client{VisitorKey: "guest"}, chat.Frame{Type: 6}); err == nil {
		t.Fatal("closed service accepted frame")
	}
	if err := f.s.Close(canceled); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestVoiceInvalidGuestIsBusinessFailureAndLeavesNoFile(t *testing.T) {
	f := setup(t)
	connection, cookie, _ := dial(t, f, nil)
	result := voice(t, f, cookie, wav(9), "javascript:bad()")
	if result.Flag || result.Code != platform.CodeInvalid {
		t.Fatalf("invalid guest was not a business input failure: %#v", result)
	}
	if count(t, f, "SELECT count(*) FROM tb_chat_record") != 0 {
		t.Fatal("invalid guest persisted voice")
	}
	objects, err := filepath.Glob(filepath.Join(f.cfg.DataDir, "files", "voice", "*.wav"))
	must(t, err)
	if len(objects) > 0 {
		t.Fatal("invalid guest voice file was not compensated")
	}
	must(t, connection.WriteJSON(chat.Frame{Type: 6}))
	if read(t, connection).Type != 6 {
		t.Fatal("invalid guest broadcast voice")
	}
}

func TestRestartPreservesOldFactsAndReadsExactHistoryWindow(t *testing.T) {
	f := setup(t)
	for _, at := range []time.Time{now.Add(-2 * time.Hour), now.Add(-12 * time.Hour), now.Add(-12*time.Hour - time.Second), now.Add(time.Hour)} {
		execute(t, f.db, "INSERT INTO tb_chat_record(nickname,avatar,content,ip_address,ip_source,type,create_time,owner_key) VALUES('history','/history.png','saved','127.0.0.1','',3,?,'g:history')", stamp(at))
	}
	timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	must(t, f.s.Close(timeout))
	must(t, f.db.Close())
	db, err := platform.OpenSQLite(f.cfg)
	must(t, err)
	clock := func() time.Time { return now }
	service := chat.New(db, f.cfg, files.New(db, f.cfg, nil, clock), analytics.New(db, f.cfg, nil, clock), nil, clock)
	server := httptest.NewServer(http.HandlerFunc(service.ServeWebSocket))
	t.Cleanup(func() {
		timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := service.Close(timeout); err != nil {
			t.Error(err)
		}
		server.Close()
		db.Close()
	})
	c, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/websocket", nil)
	must(t, err)
	defer c.Close()
	response.Body.Close()
	history := object(t, until(t, c, 2))["chatRecordList"].([]any)
	if len(history) != 2 || history[0].(map[string]any)["createTime"] != stamp(now.Add(-12*time.Hour)) || history[1].(map[string]any)["createTime"] != stamp(now.Add(-2*time.Hour)) {
		t.Fatalf("restart history boundary/order=%v", history)
	}
	var persisted int
	must(t, db.QueryRow("SELECT count(*) FROM tb_chat_record").Scan(&persisted))
	if persisted != 4 {
		t.Fatal("restart deleted old or future business facts")
	}
}
