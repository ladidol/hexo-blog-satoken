package identity_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
	"golang.org/x/crypto/bcrypt"
)

var ctx = context.Background()

const password = "correct-password-123"

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *clock) advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.at = c.at.Add(d) }

type sender struct {
	mu       sync.Mutex
	messages []mail.Message
	status   string
	err      error
}

func (s *sender) Submit(ctx context.Context, m mail.Message) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, m)
	return s.status, s.err
}

type fixture struct {
	s     *identity.Service
	db    *sql.DB
	cfg   platform.Config
	clock *clock
	mail  *sender
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{clock: &clock{at: time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)}, mail: &sender{status: "accepted"}}
	f.cfg = platform.Config{DataDir: t.TempDir(), PublicURL: "http://localhost:8888", FrontendURL: "http://localhost:8080", TimeZone: time.FixedZone("Asia/Shanghai", 8*3600), Values: map[string]string{}}
	var err error
	f.db, err = platform.OpenSQLite(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Close() })
	err = platform.Migrate(ctx, f.db, []string{identity.Schema, catalog.Schema, article.Schema, community.Schema, media.Schema, site.Schema, analytics.Schema, mail.Schema, chat.Schema, engagement.Schema}, func(ctx context.Context, tx *sql.Tx) error { return bootstrap.Seed(ctx, tx, f.cfg) })
	if err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		t.Fatal(err)
	}
	// Keep a legacy ordinary username fixture; changing the new-database default
	// must not remove support for already deployed local-admin accounts.
	execute(t, f.db, "UPDATE tb_user_auth SET username='local-admin',password=? WHERE id=1", string(hash))
	execute(t, f.db, `INSERT INTO tb_user_info(id,email,nickname,avatar,create_time) VALUES(11,'user@example.test','Normal','http://example.test/avatar.png','2026-09-12T16:00:00');
		INSERT INTO tb_user_auth(id,user_info_id,username,password,login_type,create_time) VALUES(101,11,'user@example.test','',1,'2026-09-12T16:00:00');
		INSERT INTO tb_user_role(user_id,role_id) VALUES(11,2)`)
	execute(t, f.db, "UPDATE tb_user_auth SET password=? WHERE id=101", string(hash))
	f.s = newService(f, nil)
	return f
}
func newService(f *fixture, client *http.Client) *identity.Service {
	return identity.New(f.db, f.cfg, files.New(f.db, f.cfg, client, f.clock.now), mail.New(f.db, f.cfg, f.mail, f.clock.now), social.New(f.db, f.cfg, client, f.clock.now), site.New(f.db, f.cfg, f.clock.now), analytics.New(f.db, f.cfg, client, f.clock.now), engagement.New(f.db, f.cfg, f.clock.now), f.clock.now)
}
func execute(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func input(body any) platform.Input {
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return platform.Input{Body: data, Query: url.Values{}, Form: url.Values{}, Params: map[string]string{}}
}
func requireCode(t *testing.T, err error, code int) {
	t.Helper()
	var e *platform.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("error %v, want code %d", err, code)
	}
}
func object(t *testing.T, v any) platform.Object {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out platform.Object
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func login(t *testing.T, f *fixture, user, visitor string) (*http.Cookie, platform.Actor, platform.Object) {
	t.Helper()
	in := input(nil)
	in.Body = nil
	in.Form = url.Values{"username": {user}, "password": {password}}
	in.VisitorKey = visitor
	in.IP = "127.0.0.1"
	in.Browser = "Firefox"
	in.OS = "Linux"
	w := httptest.NewRecorder()
	in.Response = w
	v, err := f.s.Login(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	var token *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == platform.TokenCookie {
			token = c
		}
	}
	if token == nil || token.Value == "" || !token.HttpOnly || token.Path != "/" || token.MaxAge != 36000 {
		t.Fatalf("login cookie invalid: %v", token)
	}
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	r.AddCookie(token)
	r.AddCookie(&http.Cookie{Name: platform.VisitorCookie, Value: visitor})
	a, err := f.s.Resolve(r)
	if err != nil || !a.LoggedIn {
		t.Fatalf("resolve=%v %v", a, err)
	}
	return token, a, object(t, v)
}
func resolve(t *testing.T, s *identity.Service, token *http.Cookie) platform.Actor {
	t.Helper()
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(token)
	a, err := s.Resolve(r)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPasswordsEnforceBytesAndBcryptWithoutLeakingSecrets(t *testing.T) {
	for _, v := range []string{"short", strings.Repeat("a", 73), string([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})} {
		if _, err := identity.HashPassword(v); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
	for _, v := range []string{password, strings.Repeat("界", 24)} {
		hash, err := identity.HashPassword(v)
		if err != nil {
			t.Fatal(err)
		}
		cost, err := bcrypt.Cost([]byte(hash))
		if err != nil || cost != 10 {
			t.Fatal("wrong bcrypt cost")
		}
		if err := identity.VerifyPassword(hash, v); err != nil {
			t.Fatal(err)
		}
		if err := identity.VerifyPassword(hash, v+"x"); err == nil {
			t.Fatal("wrong password accepted")
		}
	}
}

func TestIdentityLoginSessionsAndProfilesStayInCorrectIDDomain(t *testing.T) {
	f := setup(t)
	one, a, detail := login(t, f, "USER@example.test", "browser-a")
	if a.AuthID != 101 || a.UserInfoID != 11 || detail["id"] != float64(101) || detail["userInfoId"] != float64(11) || detail["password"] != nil {
		t.Fatalf("bad login projection: %v %v", a, detail)
	}
	for _, key := range []string{"articleLikeSet", "commentLikeSet", "talkLikeSet", "roleList"} {
		if _, ok := detail[key].([]any); !ok {
			t.Fatalf("%s is not an array", key)
		}
	}
	two, _, _ := login(t, f, "user@example.test", "browser-b")
	if two.Value == one.Value {
		t.Fatal("two sessions share token")
	}
	update := input(map[string]any{"nickname": "Changed", "intro": "Bio", "webSite": "https://example.test", "userInfoId": 1})
	update.Actor = a
	if _, err := f.s.UpdateUserInfo(ctx, update); err != nil {
		t.Fatal(err)
	}
	if actor := resolve(t, f.s, one); actor.Nickname != "Changed" || actor.UserInfoID != 11 {
		t.Fatal("session cached profile or modified wrong owner")
	}
	if actor, err := f.s.ActorForVisitor(ctx, "browser-a"); err != nil || !actor.LoggedIn || actor.Nickname != "Changed" {
		t.Fatalf("visitor identity=%v %v", actor, err)
	}
	if actor, err := f.s.ActorForVisitor(ctx, "unbound-browser"); err != nil || actor.LoggedIn {
		t.Fatal("visitor cookie grants login")
	}
	out := platform.Input{Request: httptest.NewRequest("POST", "/logout", nil), Response: httptest.NewRecorder()}
	out.Request.AddCookie(one)
	if _, err := f.s.Logout(ctx, out); err != nil {
		t.Fatal(err)
	}
	if resolve(t, f.s, one).LoggedIn || !resolve(t, f.s, two).LoggedIn {
		t.Fatal("logout did not revoke only current token")
	}
	if actor, err := f.s.ActorForVisitor(ctx, "browser-a"); err != nil || actor.LoggedIn {
		t.Fatal("visitor retained logout identity")
	}
	f.clock.advance(10 * time.Hour)
	if resolve(t, f.s, two).LoggedIn {
		t.Fatal("absolute session expiry not enforced")
	}
	if resolve(t, newService(f, nil), one).LoggedIn {
		t.Fatal("sessions persisted across restart")
	}
}

func TestIdentityRejectsWrongPasswordAndDisabledUser(t *testing.T) {
	f := setup(t)
	in := input(map[string]any{"username": "user@example.test", "password": "wrong-password"})
	in.Response = httptest.NewRecorder()
	if _, err := f.s.Login(ctx, in); err == nil {
		t.Fatal("wrong password accepted")
	}
	execute(t, f.db, "UPDATE tb_user_info SET is_disable=1 WHERE id=11")
	in.Body = []byte(`{"username":"user@example.test","password":"correct-password-123"}`)
	if _, err := f.s.Login(ctx, in); err == nil {
		t.Fatal("disabled user logged in")
	}
	if _, err := f.s.UpdateUserInfo(ctx, input(map[string]any{"nickname": "attack"})); err == nil {
		t.Fatal("anonymous changed profile")
	}
}

func TestProfileOmittedOptionalFieldsPreserveExistingValues(t *testing.T) {
	f := setup(t)
	_, user, _ := login(t, f, "user@example.test", "user")
	execute(t, f.db, "UPDATE tb_user_info SET intro='existing bio',web_site='https://example.test' WHERE id=11")
	in := input(map[string]any{"nickname": "Changed"})
	in.Actor = user
	if _, err := f.s.UpdateUserInfo(ctx, in); err != nil {
		t.Fatal(err)
	}
	var bio, website string
	if err := f.db.QueryRow("SELECT intro,web_site FROM tb_user_info WHERE id=11").Scan(&bio, &website); err != nil || bio != "existing bio" || website != "https://example.test" {
		t.Fatal("omitted optional fields erased profile values")
	}
}

func sendCode(t *testing.T, f *fixture, email, ip string) string {
	t.Helper()
	in := platform.Input{Query: url.Values{"username": {email}}, IP: ip}
	value, err := f.s.SendCode(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	f.mail.mu.Lock()
	message := f.mail.messages[len(f.mail.messages)-1]
	f.mail.mu.Unlock()
	code := regexp.MustCompile(`[0-9]{6}`).FindString(message.Text)
	if code == "" {
		t.Fatal("verification email lacks six-digit code")
	}
	encoded, _ := json.Marshal(value)
	if strings.Contains(string(encoded), code) {
		t.Fatal("code leaked in response")
	}
	return code
}
func TestVerificationAcceptedDeliveryAtomicConsumptionAndRecovery(t *testing.T) {
	f := setup(t)
	code := sendCode(t, f, " New@Example.test ", "127.0.0.1")
	if _, err := f.s.SendCode(ctx, platform.Input{Query: url.Values{"username": {"another@example.test"}}, IP: "127.0.0.1"}); err == nil {
		t.Fatal("same IP requested codes within 60 seconds")
	}
	payload := map[string]any{"username": "new@example.test", "password": password, "code": "wrong"}
	if _, err := f.s.Register(ctx, input(payload)); err == nil {
		t.Fatal("wrong code accepted")
	}
	payload["code"] = code
	execute(t, f.db, `CREATE TRIGGER reject_register BEFORE INSERT ON tb_user_info BEGIN SELECT RAISE(ABORT,'register rejected'); END`)
	if _, err := f.s.Register(ctx, input(payload)); err == nil {
		t.Fatal("database failure masked")
	}
	execute(t, f.db, "DROP TRIGGER reject_register")
	payload["roleIdList"] = []int{1}
	if _, err := f.s.Register(ctx, input(payload)); err == nil {
		t.Fatal("registration role injection accepted")
	}
	delete(payload, "roleIdList")
	var workers sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); _, err := f.s.Register(ctx, input(payload)); results <- err }()
	}
	workers.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("single code produced %d successful registrations", success)
	}
	_, a, detail := login(t, f, "new@example.test", "registered")
	roles := detail["roleList"].([]any)
	if len(roles) != 1 || roles[0] != "user" {
		t.Fatalf("registered roles: %v", roles)
	}
	first, _, _ := login(t, f, "new@example.test", "first")
	second, _, _ := login(t, f, "new@example.test", "second")
	f.clock.advance(time.Minute)
	code = sendCode(t, f, "NEW@EXAMPLE.TEST", "127.0.0.1")
	reset := input(map[string]any{"username": "new@example.test", "password": "replacement-password", "code": code})
	if _, err := f.s.UpdatePassword(ctx, reset); err != nil {
		t.Fatal(err)
	}
	if resolve(t, f.s, first).LoggedIn || resolve(t, f.s, second).LoggedIn {
		t.Fatal("password reset did not revoke all sessions")
	}
	if _, err := f.s.UpdatePassword(ctx, reset); err == nil {
		t.Fatal("consumed code replayed")
	}
	var hash string
	if err := f.db.QueryRow("SELECT password FROM tb_user_auth WHERE user_info_id=?", a.UserInfoID).Scan(&hash); err != nil || identity.VerifyPassword(hash, "replacement-password") != nil {
		t.Fatal("reset password not persisted")
	}
	f.clock.advance(time.Minute)
	code = sendCode(t, f, "bind@example.test", "127.0.0.1")
	_, owner, _ := login(t, f, "user@example.test", "bind-owner")
	binding := input(map[string]any{"email": "BIND@example.test", "code": code})
	binding.Actor = owner
	if _, err := f.s.SaveUserEmail(ctx, binding); err != nil {
		t.Fatal(err)
	}
	var email string
	if err := f.db.QueryRow("SELECT email FROM tb_user_info WHERE id=11").Scan(&email); err != nil || email != "bind@example.test" {
		t.Fatalf("bound email=%s %v", email, err)
	}
	if _, err := f.s.SaveUserEmail(ctx, binding); err == nil {
		t.Fatal("binding reused consumed code")
	}
}

func TestVerificationFailureAndExpiryNeverActivateUsableCodes(t *testing.T) {
	f := setup(t)
	f.mail.status = "failed"
	f.mail.err = errors.New("controlled SMTP refusal")
	_, err := f.s.SendCode(ctx, platform.Input{Query: url.Values{"username": {"failed@example.test"}}, IP: "a"})
	if err == nil {
		t.Fatal("failed SMTP was reported successful")
	}
	f.mail.mu.Lock()
	code := regexp.MustCompile(`[0-9]{6}`).FindString(f.mail.messages[0].Text)
	f.mail.mu.Unlock()
	if _, err := f.s.Register(ctx, input(map[string]any{"username": "failed@example.test", "password": password, "code": code})); err == nil {
		t.Fatal("failed delivery activated code")
	}
	f.mail.status = "accepted"
	f.mail.err = nil
	code = sendCode(t, f, "expired@example.test", "b")
	f.clock.advance(15 * time.Minute)
	if _, err := f.s.Register(ctx, input(map[string]any{"username": "expired@example.test", "password": password, "code": code})); err == nil {
		t.Fatal("expired code accepted")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSocialLoginsUseOnlyVerifiedIdentitiesWithoutPersistingCredentials(t *testing.T) {
	f := setup(t)
	f.cfg.Values = map[string]string{"BLOG_QQ_APP_ID": "app", "BLOG_WEIBO_APP_KEY": "123", "BLOG_WEIBO_APP_SECRET": "private-app-secret", "BLOG_WEIBO_REDIRECT_URI": "https://example.test/callback"}
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/oauth2.0/me":
			body = `{"client_id":"app","openid":"qq-open-id"}`
		case "/user/get_user_info":
			body = `{"ret":0,"nickname":"QQ user","figureurl_qq_2":"https://example.test/qq.png"}`
		case "/oauth2/access_token":
			body = `{"access_token":"private-provider-token","uid":"42"}`
		case "/oauth2/get_token_info":
			body = `{"appkey":"123","uid":"42","expire_in":3600}`
		case "/2/users/show.json":
			body = `{"idstr":"42","screen_name":"Weibo user","avatar_hd":"https://example.test/weibo.png"}`
		default:
			t.Fatalf("unexpected provider path %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	f.s = newService(f, client)
	for _, typ := range []int{2, 3} {
		var firstID any
		for attempt := 0; attempt < 2; attempt++ {
			in := input(map[string]any{"openId": "qq-open-id", "accessToken": "private-input-token", "code": "private-input-code"})
			in.Response = httptest.NewRecorder()
			in.VisitorKey = "social-browser"
			var v any
			var err error
			if typ == 2 {
				v, err = f.s.QqLogin(ctx, in)
			} else {
				v, err = f.s.WeiboLogin(ctx, in)
			}
			if err != nil {
				t.Fatal(err)
			}
			profile := object(t, v)
			if profile["loginType"] != float64(typ) {
				t.Fatal("wrong login provider")
			}
			if attempt == 0 {
				firstID = profile["id"]
			} else if profile["id"] != firstID {
				t.Fatal("social login created duplicate account")
			}
			raw, _ := json.Marshal(v)
			for _, secret := range []string{"private-input-token", "private-provider-token", "private-input-code", "private-app-secret", "password"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("social response leaked credentials")
				}
			}
		}
	}
	if number(t, f, "SELECT count(*) FROM tb_user_auth WHERE login_type IN (2,3)") != 2 || number(t, f, "SELECT count(*) FROM tb_user_auth WHERE login_type IN (2,3) AND password<>''") != 0 {
		t.Fatal("provider token persisted or duplicate identity created")
	}
	bad := input(map[string]any{"openId": "forged", "accessToken": "private-input-token"})
	bad.Response = httptest.NewRecorder()
	if _, err := f.s.QqLogin(ctx, bad); err == nil {
		t.Fatal("unverified QQ ID created account")
	}
	f.cfg.Values = map[string]string{}
	f.s = newService(f, nil)
	if _, err := f.s.WeiboLogin(ctx, input(map[string]any{"code": "x"})); err == nil {
		t.Fatal("unconfigured provider reported success")
	}
}

func avatarInput(t *testing.T, a platform.Actor, shade uint8) platform.Input {
	t.Helper()
	var data bytes.Buffer
	writer := multipart.NewWriter(&data)
	part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="avatar.png"`}, "Content-Type": {"image/png"}})
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: shade, A: 255})
	if err := png.Encode(part, img); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/users/avatar", &data)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.MultipartForm.RemoveAll() })
	return platform.Input{Actor: a, Files: r.MultipartForm.File["file"]}
}
func TestAvatarPersistenceCompensationAndAdminPasswordRevocation(t *testing.T) {
	f := setup(t)
	_, user, _ := login(t, f, "user@example.test", "user")
	one, admin, _ := login(t, f, "local-admin", "admin-one")
	two, _, _ := login(t, f, "local-admin", "admin-two")
	v, err := f.s.UpdateUserAvatar(ctx, avatarInput(t, user, 20))
	if err != nil {
		t.Fatal(err)
	}
	var avatar string
	if err := f.db.QueryRow("SELECT avatar FROM tb_user_info WHERE id=11").Scan(&avatar); err != nil || avatar != v {
		t.Fatal("avatar update missing")
	}
	execute(t, f.db, `CREATE TRIGGER reject_avatar BEFORE UPDATE OF avatar ON tb_user_info BEGIN SELECT RAISE(ABORT,'avatar rejected'); END`)
	if _, err := f.s.UpdateUserAvatar(ctx, avatarInput(t, user, 21)); err == nil {
		t.Fatal("database failure masked")
	}
	files, err := filepath.Glob(filepath.Join(f.cfg.DataDir, "files", "avatar", "*.png"))
	if err != nil || len(files) != 1 {
		t.Fatalf("failed avatar left files: %d %v", len(files), err)
	}
	execute(t, f.db, "DROP TRIGGER reject_avatar")
	bad := adminInput(map[string]any{"oldPassword": "wrong", "newPassword": "new-admin-password"}, admin)
	if _, err := f.s.UpdateAdminPassword(ctx, bad); err == nil {
		t.Fatal("wrong old password accepted")
	}
	if !resolve(t, f.s, one).LoggedIn {
		t.Fatal("failed change revoked current session")
	}
	good := adminInput(map[string]any{"oldPassword": password, "newPassword": "new-admin-password"}, admin)
	if _, err := f.s.UpdateAdminPassword(ctx, good); err != nil {
		t.Fatal(err)
	}
	if resolve(t, f.s, one).LoggedIn || resolve(t, f.s, two).LoggedIn {
		t.Fatal("admin password change retained old sessions")
	}
	in := input(map[string]any{"username": "local-admin", "password": "new-admin-password"})
	in.Response = httptest.NewRecorder()
	if _, err := f.s.Login(ctx, in); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentVisitorRebindingKeepsOneCurrentAssociation(t *testing.T) {
	f := setup(t)
	token, _, _ := login(t, f, "user@example.test", "original")
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			r := httptest.NewRequest("GET", "/", nil)
			r.AddCookie(token)
			r.AddCookie(&http.Cookie{Name: platform.VisitorCookie, Value: fmt.Sprintf("visitor-%d", i)})
			if _, err := f.s.Resolve(r); err != nil {
				t.Error(err)
			}
		}(i)
	}
	close(start)
	workers.Wait()
	bound := 0
	for i := 0; i < 20; i++ {
		a, err := f.s.ActorForVisitor(ctx, fmt.Sprintf("visitor-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if a.LoggedIn {
			bound++
		}
	}
	if bound != 1 {
		t.Fatalf("one token retained %d visitor associations", bound)
	}
}
