package community_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"blog-go/internal/analytics"
	"blog-go/internal/article"
	"blog-go/internal/bootstrap"
	"blog-go/internal/catalog"
	"blog-go/internal/chat"
	"blog-go/internal/community"
	"blog-go/internal/engagement"
	"blog-go/internal/identity"
	"blog-go/internal/mail"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
)

type senderFunc func(context.Context, mail.Message) (string, error)

func (f senderFunc) Submit(ctx context.Context, message mail.Message) (string, error) {
	return f(ctx, message)
}

type fixture struct {
	service *community.Service
	site    *site.Service
	db      *sql.DB
	cfg     platform.Config
	clock   atomic.Int64
}

func setup(t *testing.T, sender mail.Sender) *fixture {
	t.Helper()
	f := &fixture{cfg: platform.Config{DataDir: t.TempDir(), TimeZone: time.FixedZone("Asia/Shanghai", 8*3600), PublicURL: "http://localhost:8888", FrontendURL: "http://localhost:8080", Values: map[string]string{"BLOG_UPLOAD_MODE": "local"}}}
	f.clock.Store(time.Date(2026, 9, 12, 8, 15, 0, 123456789, time.UTC).UnixNano())
	now := func() time.Time { return time.Unix(0, f.clock.Load()) }
	var err error
	f.db, err = platform.OpenSQLite(f.cfg)
	must(t, err)
	t.Cleanup(func() { _ = f.db.Close() })
	must(t, platform.Migrate(context.Background(), f.db, []string{identity.Schema, catalog.Schema, article.Schema, community.Schema, media.Schema, site.Schema, analytics.Schema, mail.Schema, engagement.Schema, chat.Schema}, func(ctx context.Context, tx *sql.Tx) error { return bootstrap.Seed(ctx, tx, f.cfg) }))
	_, err = f.db.Exec(`UPDATE tb_user_info SET email='admin@example.test' WHERE id=1;
	 INSERT INTO tb_user_info(id,email,nickname,avatar,is_disable,create_time) VALUES
	 (2,'author@example.test','作者','/author.png',0,'2026-09-12T10:00:00'),
	 (3,'reply@example.test','回复者','/reply.png',0,'2026-09-12T10:00:00'),
	 (4,'other@example.test','另一人','/other.png',0,'2026-09-12T10:00:00'),
	 (5,'disabled@example.test','禁用','/disabled.png',1,'2026-09-12T10:00:00');
	 UPDATE tb_article SET user_id=2 WHERE id=1;
	 INSERT INTO tb_article(id,user_id,article_title,article_content,status,create_time) VALUES
	 (2,2,'另一篇','内容',1,'2026-09-12T10:00:00'),(3,2,'私密','内容',2,'2026-09-12T10:00:00');
	 UPDATE tb_talk SET user_id=3 WHERE id=1;
	 INSERT INTO tb_talk(id,user_id,content,status,create_time) VALUES
	 (2,3,'私密说说',2,'2026-09-12T10:00:00');`)
	must(t, err)
	f.site = site.New(f.db, f.cfg, now)
	f.service = community.New(f.db, f.cfg, f.site, analytics.New(f.db, f.cfg, nil, now), mail.New(f.db, f.cfg, sender, now), engagement.New(f.db, f.cfg, now), now)
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func input(value any) platform.Input {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return platform.Input{Body: body, Query: make(url.Values), Params: make(map[string]string), IP: "192.0.2.1"}
}
func actor(id int64) platform.Actor {
	return platform.Actor{LoggedIn: true, UserInfoID: id, AuthID: id + 100}
}
func comment(typ int, topic any, parent any, reply any) platform.Input {
	in := input(platform.Object{"type": typ, "topicId": topic, "parentId": parent, "replyUserId": reply, "commentContent": "<p>评论 <strong>你好</strong></p>", "userId": 4})
	in.Actor = actor(2)
	return in
}
func (f *fixture) config(t *testing.T, values platform.Object) {
	t.Helper()
	_, err := f.site.UpdateWebsiteConfig(context.Background(), input(values))
	must(t, err)
}
func save(t *testing.T, f *fixture, in platform.Input) platform.Object {
	t.Helper()
	value, err := f.service.SaveComment(context.Background(), in)
	must(t, err)
	result, ok := value.(platform.Result)
	if !ok || !result.Flag || result.Code != platform.CodeOK {
		t.Fatalf("invalid saved result: %#v", value)
	}
	return result.Data.(platform.Object)
}
func page(t *testing.T, value any, err error) platform.Page {
	t.Helper()
	must(t, err)
	got, ok := value.(platform.Page)
	if !ok {
		t.Fatalf("expected page: %#v", value)
	}
	return got
}
func (f *fixture) comments(t *testing.T, typ, topic string) platform.Page {
	t.Helper()
	in := input(nil)
	in.Query = url.Values{"type": {typ}}
	if topic != "" {
		in.Query.Set("topicId", topic)
	}
	value, err := f.service.ListComments(context.Background(), in)
	return page(t, value, err)
}
func review(t *testing.T, endpoint platform.Endpoint, ids []int64, flag int) {
	t.Helper()
	_, err := endpoint(context.Background(), input(platform.Object{"idList": ids, "isReview": flag}))
	must(t, err)
}
func count(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	must(t, db.QueryRow(query, args...).Scan(&n))
	return n
}

func TestSanitizeHTMLAllowsFormattingAndRemovesActiveContent(t *testing.T) {
	raw := `<p>Hello <strong>世界</strong><script>alert(1)</script><style>bad</style><iframe>bad</iframe><a href="https://example.test/path" onclick="evil()">link</a><img src="/emoji.png" alt="笑" width="24" height="24" onerror="evil()" style="bad"><img src="data:image/svg+xml,bad"><a href="java&#x0a;script:evil()">text</a><svg><a href="javascript:evil()">bad</a></svg></p>`
	got, err := community.SanitizeHTML(raw)
	must(t, err)
	for _, bad := range []string{"script", "style", "iframe", "onclick", "onerror", "javascript:", "data:", "<svg", "alert(", "bad"} {
		if strings.Contains(got, bad) {
			t.Fatalf("unsafe content survived %q: %s", bad, got)
		}
	}
	for _, want := range []string{"<p>", "<strong>世界</strong>", `href="https://example.test/path"`, `rel="`, `src="/emoji.png"`, `alt="笑"`, `width="24"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("lost supported markup %q: %s", want, got)
		}
	}
	for _, raw := range []string{"", " <p> &nbsp; <br></p>", "<script>text</script>", `<img src="javascript:evil()">`} {
		if _, err := community.SanitizeHTML(raw); err == nil {
			t.Fatalf("empty sanitized content accepted: %q", raw)
		}
	}
	if _, err := community.SanitizeHTML(`<img src="https://example.test/emoji.gif">`); err != nil {
		t.Fatal("emoji-only content rejected", err)
	}
	for _, raw := range []string{`<p><b>one<p>two</b>`, `<math><mtext><table><mglyph><style><!--</style><img title="--><img src=x onerror=evil()>">`} {
		got, err := community.SanitizeHTML(raw)
		if err == nil && strings.Contains(got, "onerror") {
			t.Fatalf("malformed HTML unsafe: %s", got)
		}
	}
}

func TestCommentsReviewVisibilityOwnershipAndExactProjection(t *testing.T) {
	f := setup(t, nil)
	data := save(t, f, comment(1, "1", nil, nil))
	id := data["commentId"].(int64)
	if data["notification"].(platform.Object)["status"] != "skipped" {
		t.Fatal("disabled notification attempted")
	}
	if count(t, f.db, "SELECT count(*) FROM tb_comment WHERE id=? AND user_id=2 AND is_review=0", id) != 1 {
		t.Fatal("author or moderation state was supplied by caller")
	}
	if got := f.comments(t, "1", "1"); got.Count != 0 || len(got.RecordList) != 0 {
		t.Fatal("pending comment was public")
	}
	review(t, f.service.UpdateCommentsReview, []int64{id}, 1)
	got := f.comments(t, "1", "1")
	if got.Count != 1 || len(got.RecordList) != 1 {
		t.Fatalf("approved comment missing: %+v", got)
	}
	r := got.RecordList[0]
	if len(r) != 10 || r["userId"] != int64(2) || r["likeCount"] != int64(0) || r["replyCount"] != int64(0) || r["webSite"] != nil || r["createTime"] != "2026-09-12T16:15:00" || len(r["replyDTOList"].([]platform.Object)) != 0 {
		t.Fatalf("incorrect CommentDTO: %#v", r)
	}
	f.config(t, platform.Object{"isCommentReview": 0})
	save(t, f, comment(2, nil, nil, nil))
	save(t, f, comment(3, 1, nil, nil))
	if f.comments(t, "2", "").Count != 1 || f.comments(t, "3", "1").Count != 1 {
		t.Fatal("link/talk comment was not visible")
	}
	_, err := f.db.Exec("UPDATE tb_article SET status=2 WHERE id=1")
	must(t, err)
	_, err = f.service.ListComments(context.Background(), platform.Input{Query: url.Values{"type": {"1"}, "topicId": {"1"}}})
	if err == nil {
		t.Fatal("private topic comments exposed")
	}
}

func TestCommentValidationAndRepliesRemainInTheirThread(t *testing.T) {
	f := setup(t, nil)
	f.config(t, platform.Object{"isCommentReview": 0})
	root := save(t, f, comment(1, 1, nil, nil))["commentId"].(int64)
	in := comment(1, 1, root, 2)
	in.Actor = actor(3)
	reply := save(t, f, in)["commentId"].(int64)
	for _, bad := range []platform.Input{comment(1, 2, root, 2), comment(3, 1, root, 2), comment(1, 1, reply, 3), comment(1, 1, root, 4), comment(1, 3, nil, nil), comment(3, 2, nil, nil), comment(1, nil, nil, nil), comment(9, 1, nil, nil), comment(1, 1, nil, 2)} {
		if _, err := f.service.SaveComment(context.Background(), bad); err == nil {
			t.Fatalf("invalid comment accepted: %s", bad.Body)
		}
	}
	for _, who := range []platform.Actor{{}, actor(5), actor(999)} {
		in := comment(1, 1, nil, nil)
		in.Actor = who
		if _, err := f.service.SaveComment(context.Background(), in); err == nil {
			t.Fatalf("invalid author accepted: %+v", who)
		}
	}
	in = comment(1, 1, root, 3)
	in.Actor = actor(4)
	save(t, f, in)
	for i := 0; i < 3; i++ {
		save(t, f, comment(1, 1, root, 2))
	}
	got := f.comments(t, "1", "1").RecordList[0]
	if got["replyCount"] != int64(5) || len(got["replyDTOList"].([]platform.Object)) != 3 {
		t.Fatalf("reply count/preview: %#v", got)
	}
	query := platform.Input{Params: map[string]string{"commentId": fmt.Sprint(root)}, Query: url.Values{"current": {"2"}, "size": {"2"}}}
	value, err := f.service.ListRepliesByCommentId(context.Background(), query)
	must(t, err)
	replies := value.([]platform.Object)
	if len(replies) != 2 || len(replies[0]) != 12 || replies[0]["parentId"] != root {
		t.Fatalf("reply projection/page: %#v", replies)
	}
	review(t, f.service.UpdateCommentsReview, []int64{reply}, 0)
	if f.comments(t, "1", "1").RecordList[0]["replyCount"] != int64(4) {
		t.Fatal("unreviewed reply counted")
	}
	query.Query.Set("current", "9223372036854775807")
	value, err = f.service.ListRepliesByCommentId(context.Background(), query)
	must(t, err)
	if len(value.([]platform.Object)) != 0 {
		t.Fatal("overflowed reply page")
	}
}

func TestCommentLikesDeleteCascadeAndRollback(t *testing.T) {
	f := setup(t, nil)
	f.config(t, platform.Object{"isCommentReview": 0})
	root := save(t, f, comment(1, 1, nil, nil))["commentId"].(int64)
	child := save(t, f, comment(1, 1, root, 2))["commentId"].(int64)
	for _, id := range []int64{root, child} {
		_, err := f.service.SaveCommentLike(context.Background(), platform.Input{Actor: actor(3), Params: map[string]string{"commentId": fmt.Sprint(id)}})
		must(t, err)
	}
	if f.comments(t, "1", "1").RecordList[0]["likeCount"] != int64(1) {
		t.Fatal("like count not visible")
	}
	_, err := f.service.SaveCommentLike(context.Background(), platform.Input{Actor: actor(3), Params: map[string]string{"commentId": fmt.Sprint(root)}})
	must(t, err)
	if f.comments(t, "1", "1").RecordList[0]["likeCount"] != int64(0) {
		t.Fatal("like cancellation not visible")
	}
	_, err = f.db.Exec("CREATE TRIGGER reject_delete BEFORE DELETE ON tb_comment BEGIN SELECT RAISE(FAIL,'reject delete'); END")
	must(t, err)
	_, err = f.service.DeleteComments(context.Background(), input([]int64{root}))
	if err == nil {
		t.Fatal("database failure swallowed")
	}
	if count(t, f.db, "SELECT count(*) FROM tb_like WHERE target_type=2") != 1 || count(t, f.db, "SELECT count(*) FROM tb_comment") != 2 {
		t.Fatal("failed deletion partially committed")
	}
	_, err = f.db.Exec("DROP TRIGGER reject_delete")
	must(t, err)
	_, err = f.service.DeleteComments(context.Background(), input([]int64{root}))
	must(t, err)
	if count(t, f.db, "SELECT count(*) FROM tb_comment") != 0 || count(t, f.db, "SELECT count(*) FROM tb_like WHERE target_type=2") != 0 {
		t.Fatal("root deletion retained replies or likes")
	}
}

func TestCommentNotificationsFollowCommittedFactsAndRecipients(t *testing.T) {
	var db *sql.DB
	var calls []mail.Message
	f := setup(t, senderFunc(func(ctx context.Context, m mail.Message) (string, error) {
		var n int
		must(t, db.QueryRowContext(ctx, "SELECT count(*) FROM tb_comment WHERE id=?", m.BusinessID).Scan(&n))
		if n != 1 {
			t.Fatal("mail submitted before comment committed")
		}
		calls = append(calls, m)
		return "accepted", nil
	}))
	db = f.db
	f.config(t, platform.Object{"isEmailNotice": 1})
	data := save(t, f, comment(1, 1, nil, nil))
	id := data["commentId"].(int64)
	if len(calls) != 1 || calls[0].Recipient != "admin@example.test" || data["notification"].(platform.Object)["status"] != "accepted" {
		t.Fatalf("pending notification: %#v %#v", calls, data)
	}
	review(t, f.service.UpdateCommentsReview, []int64{id}, 1)
	if len(calls) != 2 || calls[1].Recipient != "author@example.test" {
		t.Fatalf("review notification: %#v", calls)
	}
	review(t, f.service.UpdateCommentsReview, []int64{id}, 1)
	if len(calls) != 2 {
		t.Fatal("repeat review resent notification")
	}
	f.config(t, platform.Object{"isCommentReview": 0})
	in := comment(1, 1, id, 2)
	in.Actor = actor(3)
	reply := save(t, f, in)["commentId"].(int64)
	if calls[len(calls)-1].Recipient != "author@example.test" {
		t.Fatal("reply target email ignored")
	}
	in = comment(1, 1, id, 3)
	save(t, f, in)
	if calls[len(calls)-1].Recipient != "reply@example.test" {
		t.Fatal("reply-to-reply recipient ignored")
	}
	if reply <= id {
		t.Fatal("reply missing")
	}
	save(t, f, comment(3, 1, nil, nil))
	if calls[len(calls)-1].Recipient != "reply@example.test" {
		t.Fatal("talk author recipient ignored")
	}
	_, err := f.db.Exec("UPDATE tb_user_info SET email=NULL WHERE id=2")
	must(t, err)
	before := len(calls)
	data = save(t, f, comment(1, 1, nil, nil))
	if len(calls) != before || data["notification"].(platform.Object)["status"] != "skipped" {
		t.Fatal("empty recipient attempted SMTP")
	}
}

func TestNotificationFailureNeverUndoesSavedComment(t *testing.T) {
	for _, status := range []string{"failed", "unknown"} {
		t.Run(status, func(t *testing.T) {
			f := setup(t, senderFunc(func(context.Context, mail.Message) (string, error) { return status, errors.New("private SMTP failure") }))
			f.config(t, platform.Object{"isEmailNotice": 1, "isCommentReview": 0})
			value, err := f.service.SaveComment(context.Background(), comment(1, 1, nil, nil))
			must(t, err)
			result := value.(platform.Result)
			data := result.Data.(platform.Object)
			notification := data["notification"].(platform.Object)
			if !result.Flag || result.Message != "评论已保存，通知未提交成功" || notification["status"] != status || notification["deliveryId"].(int64) <= 0 || f.comments(t, "1", "1").Count != 1 {
				t.Fatalf("partial-success contract: %#v", result)
			}
		})
	}
}

func TestMessagesModerationRateLimitAndPersistence(t *testing.T) {
	f := setup(t, nil)
	in := input(platform.Object{"nickname": "访客", "avatar": "/guest.png", "messageContent": strings.Repeat("好", 255), "time": 8, "ipAddress": "forged"})
	_, err := f.service.SaveMessage(context.Background(), in)
	must(t, err)
	value, err := f.service.ListMessages(context.Background(), input(nil))
	must(t, err)
	if len(value.([]platform.Object)) != 0 {
		t.Fatal("pending message public")
	}
	if count(t, f.db, "SELECT count(*) FROM tb_message WHERE ip_address='192.0.2.1' AND time=8") != 1 {
		t.Fatal("guest attributes or trusted IP lost")
	}
	_, err = f.service.SaveMessage(context.Background(), in)
	if err == nil {
		t.Fatal("same IP bypassed five-second limit")
	}
	f.clock.Add(int64(5*time.Second - time.Nanosecond))
	_, err = f.service.SaveMessage(context.Background(), in)
	if err == nil {
		t.Fatal("five-second limit rounded down")
	}
	f.clock.Add(1)
	f.config(t, platform.Object{"isMessageReview": 0})
	_, err = f.service.SaveMessage(context.Background(), in)
	must(t, err)
	review(t, f.service.UpdateMessagesReview, []int64{1}, 1)
	value, err = f.service.ListMessages(context.Background(), input(nil))
	must(t, err)
	rows := value.([]platform.Object)
	if len(rows) != 2 || len(rows[0]) != 5 {
		t.Fatalf("MessageDTO: %#v", rows)
	}
	must(t, f.db.Close())
	db, err := platform.OpenSQLite(f.cfg)
	must(t, err)
	defer db.Close()
	if count(t, db, "SELECT count(*) FROM tb_message WHERE is_review=1") != 2 {
		t.Fatal("restart lost moderation")
	}
}

func TestMessageConcurrentLimitAndValidation(t *testing.T) {
	f := setup(t, nil)
	f.config(t, platform.Object{"isMessageReview": 0})
	var wg sync.WaitGroup
	var accepted atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.service.SaveMessage(context.Background(), input(platform.Object{"nickname": "游客", "avatar": "https://example.test/a.png", "messageContent": "并发", "time": 7}))
			if err == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 || count(t, f.db, "SELECT count(*) FROM tb_message") != 1 {
		t.Fatalf("concurrent limit accepted %d", accepted.Load())
	}
	for index, value := range []platform.Object{
		{"nickname": "n", "avatar": "/a", "messageContent": strings.Repeat("好", 256), "time": 8},
		{"nickname": "n", "avatar": "data:image/svg+xml,bad", "messageContent": "x", "time": 8},
		{"nickname": " ", "avatar": "/a", "messageContent": "x", "time": 8},
		{"nickname": "n", "avatar": "/a", "messageContent": "<script>x</script>", "time": 8},
		{"nickname": "n", "avatar": "/a", "messageContent": "x"},
	} {
		in := input(value)
		// A fresh IP makes these validation assertions independent of cooldown.
		in.IP = fmt.Sprintf("192.0.2.%d", index+2)
		if _, err := f.service.SaveMessage(context.Background(), in); err == nil {
			t.Fatalf("invalid message accepted: %#v", value)
		}
	}
}

func TestAdminPagesFiltersReviewAtomicityAndMessageDelete(t *testing.T) {
	f := setup(t, nil)
	save(t, f, comment(1, 1, nil, nil))
	save(t, f, comment(3, 1, nil, nil))
	in := input(nil)
	in.Query = url.Values{"type": {"3"}, "isReview": {"0"}, "keywords": {"作者"}, "size": {"1"}}
	value, err := f.service.ListCommentBackDTO(context.Background(), in)
	got := page(t, value, err)
	if got.Count != 1 || len(got.RecordList) != 1 || len(got.RecordList[0]) != 9 || got.RecordList[0]["articleTitle"] != nil {
		t.Fatalf("admin comment projection/filter: %#v", got)
	}
	_, err = f.service.UpdateCommentsReview(context.Background(), input(platform.Object{"idList": []int64{1, 999}, "isReview": 1}))
	if err == nil || count(t, f.db, "SELECT count(*) FROM tb_comment WHERE is_review=1") != 0 {
		t.Fatal("batch review partially succeeded")
	}
	_, err = f.service.SaveMessage(context.Background(), input(platform.Object{"nickname": "查找%", "avatar": "/a", "messageContent": "hello", "time": 7}))
	must(t, err)
	in.Query = url.Values{"keywords": {"%"}, "isReview": {"0"}}
	value, err = f.service.ListMessageBackDTO(context.Background(), in)
	got = page(t, value, err)
	if got.Count != 1 || len(got.RecordList[0]) != 8 {
		t.Fatalf("admin MessageBackDTO: %#v", got)
	}
	in.Query.Set("current", "9223372036854775807")
	value, err = f.service.ListMessageBackDTO(context.Background(), in)
	got = page(t, value, err)
	if got.Count != 1 || len(got.RecordList) != 0 {
		t.Fatal("oversized page overflowed")
	}
	_, err = f.service.DeleteMessages(context.Background(), input([]int64{1}))
	must(t, err)
	if count(t, f.db, "SELECT count(*) FROM tb_message") != 0 {
		t.Fatal("message not deleted")
	}
}

func TestCommunityRoutesEnforceIdentityAndPreserveSavedFailureEnvelope(t *testing.T) {
	f := setup(t, nil)
	f.config(t, platform.Object{"isEmailNotice": 1, "isCommentReview": 0})
	routes := f.service.Routes()
	if len(routes) != len(community.RouteSpecs) {
		t.Fatal("routes missing")
	}
	for i, r := range routes {
		if r.Spec != community.RouteSpecs[i] || r.Handle == nil {
			t.Fatal("route metadata diverged")
		}
	}
	engine, err := platform.NewRouter(routes, platform.Callbacks{Resolve: func(r *http.Request) (platform.Actor, error) {
		if r.Header.Get("X-Test-User") == "yes" {
			return actor(2), nil
		}
		return platform.Actor{}, nil
	}, Authorize: func(context.Context, platform.Actor, string, string) error {
		return &platform.Error{Code: platform.CodeFail, Message: "denied"}
	}}, f.cfg)
	must(t, err)
	for _, logged := range []bool{false, true} {
		request := httptest.NewRequest("POST", "/comments", strings.NewReader(string(comment(1, "1", nil, nil).Body)))
		request.Header.Set("Content-Type", "application/json")
		if logged {
			request.Header.Set("X-Test-User", "yes")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		var result platform.Result
		must(t, json.Unmarshal(response.Body.Bytes(), &result))
		if result.Flag != logged {
			t.Fatalf("identity/result mismatch: %s", response.Body)
		}
		if logged && result.Message != "评论已保存，通知未提交成功" {
			t.Fatalf("saved failure envelope lost: %s", response.Body)
		}
	}
	request := httptest.NewRequest("GET", "/admin/comments", nil)
	request.Header.Set("X-Test-User", "yes")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if strings.Contains(response.Body.String(), `"flag":true`) {
		t.Fatal("admin authorization bypassed")
	}
}

func TestAcceptedMailRecordingFailurePreservesCommentAndDoesNotRetry(t *testing.T) {
	var calls int
	f := setup(t, senderFunc(func(context.Context, mail.Message) (string, error) { calls++; return "accepted", nil }))
	f.config(t, platform.Object{"isEmailNotice": 1, "isCommentReview": 0})
	_, err := f.db.Exec("CREATE TRIGGER reject_mail_audit BEFORE INSERT ON tb_mail_delivery BEGIN SELECT RAISE(FAIL,'recording failed'); END")
	must(t, err)
	value, err := f.service.SaveComment(context.Background(), comment(1, 1, nil, nil))
	must(t, err)
	result := value.(platform.Result)
	notification := result.Data.(platform.Object)["notification"].(platform.Object)
	if !result.Flag || notification["status"] != "unknown" || notification["deliveryId"] != int64(0) || calls != 1 || f.comments(t, "1", "1").Count != 1 {
		t.Fatalf("accepted/unrecorded notification corrupted business result: %#v, calls=%d", result, calls)
	}
	_, err = f.db.Exec("CREATE TRIGGER reject_comment BEFORE INSERT ON tb_comment BEGIN SELECT RAISE(FAIL,'save failed'); END")
	must(t, err)
	_, err = f.service.SaveComment(context.Background(), comment(1, 1, nil, nil))
	if err == nil || calls != 1 || count(t, f.db, "SELECT count(*) FROM tb_comment") != 1 {
		t.Fatal("rolled-back comment caused a notification")
	}
}

func TestConcurrentReplyAndRootDeleteCannotLeaveOrphanState(t *testing.T) {
	f := setup(t, nil)
	f.config(t, platform.Object{"isCommentReview": 0})
	root := save(t, f, comment(1, 1, nil, nil))["commentId"].(int64)
	start := make(chan struct{})
	finished := make(chan error, 2)
	go func() {
		<-start
		_, err := f.service.SaveComment(context.Background(), comment(1, 1, root, 2))
		finished <- err
	}()
	go func() {
		<-start
		_, err := f.service.DeleteComments(context.Background(), input([]int64{root}))
		finished <- err
	}()
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-finished; err != nil {
			var business *platform.Error
			if !errors.As(err, &business) || business.Code != platform.CodeFail {
				t.Fatal(err)
			}
		}
	}
	if count(t, f.db, "SELECT count(*) FROM tb_comment") != 0 {
		t.Fatal("reply escaped concurrent root deletion")
	}
}

func TestReplyPreviewPreservesOriginalCreationTimeOrder(t *testing.T) {
	f := setup(t, nil)
	f.config(t, platform.Object{"isCommentReview": 0})
	root := save(t, f, comment(1, 1, nil, nil))["commentId"].(int64)
	first := save(t, f, comment(1, 1, root, 2))["commentId"].(int64)
	f.clock.Add(int64(10 * time.Second))
	save(t, f, comment(1, 1, root, 2))
	f.clock.Add(int64(-20 * time.Second))
	earlier := save(t, f, comment(1, 1, root, 2))["commentId"].(int64)
	preview := f.comments(t, "1", "1").RecordList[0]["replyDTOList"].([]platform.Object)
	if preview[0]["id"] != earlier || preview[1]["id"] != first {
		t.Fatalf("preview creation-time ordering changed: %#v", preview)
	}
}

func TestCommunityInputBoundariesPaginationAndDeletedVisibility(t *testing.T) {
	f := setup(t, nil)
	f.config(t, platform.Object{"isCommentReview": 0})
	in := input(platform.Object{"type": 1, "topicId": 1, "commentContent": strings.Repeat("好", 20000)})
	in.Actor = actor(2)
	first := save(t, f, in)["commentId"].(int64)
	tooLong := input(platform.Object{"type": 1, "topicId": 1, "commentContent": strings.Repeat("好", 20001)})
	tooLong.Actor = actor(2)
	if _, err := f.service.SaveComment(context.Background(), tooLong); err == nil {
		t.Fatal("comment rune limit ignored")
	}
	second := save(t, f, comment(1, 1, nil, nil))["commentId"].(int64)
	query := platform.Input{Query: url.Values{"type": {"1"}, "topicId": {"1"}, "current": {"2"}, "size": {"1"}}}
	value, err := f.service.ListComments(context.Background(), query)
	got := page(t, value, err)
	if got.Count != 2 || len(got.RecordList) != 1 || got.RecordList[0]["id"] != first {
		t.Fatalf("root pagination/order changed: %#v", got)
	}
	_, err = f.db.Exec("UPDATE tb_comment SET is_delete=1 WHERE id=?", second)
	must(t, err)
	if f.comments(t, "1", "1").Count != 1 {
		t.Fatal("deleted comment remained public")
	}
	_, err = f.service.SaveCommentLike(context.Background(), platform.Input{Actor: actor(2), Params: map[string]string{"commentId": fmt.Sprint(second)}})
	if err == nil {
		t.Fatal("deleted comment accepted a like")
	}
	for _, raw := range []string{`null`, `[]`, `{}`, `{"type":null}`, `{"type":1,"topicId":1.5}`, `{"type":1,"topicId":1,"commentContent":"<style>x</style>"}`} {
		if _, err := f.service.SaveComment(context.Background(), platform.Input{Actor: actor(2), Body: json.RawMessage(raw)}); err == nil {
			t.Fatalf("malformed comment accepted: %s", raw)
		}
	}
	for _, values := range []url.Values{{"type": {"1"}, "topicId": {"1"}, "size": {"0"}}, {"type": {"1"}, "topicId": {"1"}, "size": {"101"}}, {"type": {"1", "2"}, "topicId": {"1"}}} {
		if _, err := f.service.ListComments(context.Background(), platform.Input{Query: values}); err == nil {
			t.Fatalf("invalid query accepted: %#v", values)
		}
	}
	for _, endpoint := range []platform.Endpoint{f.service.UpdateCommentsReview, f.service.UpdateMessagesReview} {
		for _, raw := range []string{`{"idList":[],"isReview":1}`, `{"idList":[1],"isReview":2}`, `{"idList":[-1],"isReview":1}`, `{"idList":[1],"isReview":null}`} {
			if _, err := endpoint(context.Background(), platform.Input{Body: json.RawMessage(raw)}); err == nil {
				t.Fatalf("invalid review accepted: %s", raw)
			}
		}
	}
	for _, raw := range []string{strings.Repeat("<div>", 300) + "hello" + strings.Repeat("</div>", 300), string([]byte{0xff})} {
		if _, err := community.SanitizeHTML(raw); err == nil {
			t.Fatal("invalid or overdeep HTML accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.service.SaveComment(ctx, comment(1, 1, nil, nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation identity lost: %v", err)
	}
}
