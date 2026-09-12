package engagement_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"blog-go/internal/article"
	"blog-go/internal/catalog"
	"blog-go/internal/community"
	"blog-go/internal/engagement"
	"blog-go/internal/identity"
	"blog-go/internal/media"
	"blog-go/internal/platform"
)

var fixedTime = time.Date(2026, 9, 12, 2, 3, 4, 0, time.UTC)

func fixture(t *testing.T) (*sql.DB, platform.Config, *engagement.Service) {
	t.Helper()
	cfg := platform.Config{DataDir: t.TempDir(), TimeZone: time.FixedZone("Asia/Shanghai", 8*60*60)}
	db := openDB(t, cfg)
	err := platform.Migrate(context.Background(), db, []string{
		identity.Schema, catalog.Schema, article.Schema, community.Schema, media.Schema, engagement.Schema,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, `
		INSERT INTO tb_user_info(id,nickname,is_disable,create_time) VALUES
		(11,'first',0,'2026-09-12T10:00:00'), (22,'second',0,'2026-09-12T10:00:00'),
		(33,'disabled',1,'2026-09-12T10:00:00');
		INSERT INTO tb_user_auth(id,user_info_id,username,password,login_type,create_time) VALUES
		(101,11,'first-login','unused',1,'2026-09-12T10:00:00'),
		(202,22,'second-login','unused',1,'2026-09-12T10:00:00');
		INSERT INTO tb_article(id,user_id,article_title,article_content,status,is_delete,create_time) VALUES
		(101,11,'public','body',1,0,'2026-09-12T10:00:00'),
		(102,11,'another','body',1,0,'2026-09-12T10:00:00'),
		(103,11,'private','body',2,0,'2026-09-12T10:00:00'),
		(104,11,'draft','body',3,0,'2026-09-12T10:00:00'),
		(105,11,'deleted','body',1,1,'2026-09-12T10:00:00');
		INSERT INTO tb_talk(id,user_id,content,status,create_time) VALUES
		(301,11,'public',1,'2026-09-12T10:00:00'), (302,11,'private',2,'2026-09-12T10:00:00');
		INSERT INTO tb_comment(id,user_id,topic_id,comment_content,type,is_review,is_delete,create_time) VALUES
		(201,11,101,'approved',1,1,0,'2026-09-12T10:00:00'),
		(202,11,101,'pending',1,0,0,'2026-09-12T10:00:00'),
		(203,11,101,'deleted',1,1,1,'2026-09-12T10:00:00'),
		(204,11,NULL,'friend links',2,1,0,'2026-09-12T10:00:00'),
		(205,11,301,'talk comment',3,1,0,'2026-09-12T10:00:00'),
		(206,11,103,'private article comment',1,1,0,'2026-09-12T10:00:00'),
		(207,11,302,'private talk comment',3,1,0,'2026-09-12T10:00:00');`)
	return db, cfg, engagement.New(db, cfg, func() time.Time { return fixedTime })
}

func openDB(t *testing.T, cfg platform.Config) *sql.DB {
	t.Helper()
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func requireCode(t *testing.T, err error, code int) {
	t.Helper()
	var business *platform.Error
	if !errors.As(err, &business) || business.Code != code {
		t.Fatalf("error=%v, want business code %d", err, code)
	}
}

func requireCount(t *testing.T, s *engagement.Service, typ int, id, want int64) {
	t.Helper()
	got, err := s.Count(context.Background(), typ, id)
	if err != nil || got != want {
		t.Fatalf("count(%d,%d)=%d, %v; want %d", typ, id, got, err, want)
	}
}

func requireToggle(t *testing.T, s *engagement.Service, user int64, typ int, id int64, liked bool, count int64) {
	t.Helper()
	gotLiked, gotCount, err := s.Toggle(context.Background(), user, typ, id)
	if err != nil || gotLiked != liked || gotCount != count {
		t.Fatalf("toggle(%d,%d,%d)=(%v,%d,%v), want (%v,%d,nil)", user, typ, id, gotLiked, gotCount, err, liked, count)
	}
}

func TestLikesUseUserInfoIdentityAndNumericSets(t *testing.T) {
	db, _, s := fixture(t)
	ctx := context.Background()
	sets, err := s.UserSets(ctx, 11)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(sets)
	if err != nil || string(encoded) != `{"articleLikeSet":[],"commentLikeSet":[],"talkLikeSet":[]}` {
		t.Fatalf("empty sets must be arrays: %s %v", encoded, err)
	}
	requireCount(t, s, 1, 999, 0)
	requireToggle(t, s, 11, 1, 101, true, 1)
	requireToggle(t, s, 11, 2, 201, true, 1)
	requireToggle(t, s, 11, 3, 301, true, 1)
	requireToggle(t, s, 11, 2, 204, true, 1)
	requireToggle(t, s, 11, 2, 205, true, 1)
	requireToggle(t, s, 22, 1, 101, true, 2)
	sets, err = s.UserSets(ctx, 11)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(sets)
	if err != nil || string(encoded) != `{"articleLikeSet":[101],"commentLikeSet":[201,204,205],"talkLikeSet":[301]}` {
		t.Fatalf("login sets must use numeric IDs: %s %v", encoded, err)
	}
	var owner int64
	var stamp string
	if err := db.QueryRow("SELECT user_info_id,create_time FROM tb_like WHERE target_type=2 AND target_id=201").Scan(&owner, &stamp); err != nil || owner != 11 || stamp != "2026-09-12T10:03:04" {
		t.Fatalf("wrong identity/time: %d %s %v", owner, stamp, err)
	}
	requireToggle(t, s, 11, 1, 101, false, 1)
	requireCount(t, s, 1, 101, 1)
	sets, err = s.UserSets(ctx, 11)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(sets["articleLikeSet"])
	if string(encoded) != "[]" {
		t.Fatalf("canceled like remains in login set: %s", encoded)
	}
}

func TestLikesRejectUnavailableTargetsAndInvalidUsers(t *testing.T) {
	db, _, s := fixture(t)
	ctx := context.Background()
	for _, target := range []struct {
		typ int
		id  int64
	}{
		{1, 103}, {1, 104}, {1, 105}, {1, 999}, {2, 202}, {2, 203},
		{2, 206}, {2, 207}, {2, 999}, {3, 302}, {3, 999},
	} {
		t.Run(fmt.Sprintf("target-%d-%d", target.typ, target.id), func(t *testing.T) {
			_, _, err := s.Toggle(ctx, 11, target.typ, target.id)
			requireCode(t, err, platform.CodeMissing)
			requireCount(t, s, target.typ, target.id, 0)
		})
	}
	for _, user := range []int64{0, -1, 101, 33} {
		_, _, err := s.Toggle(ctx, user, 1, 101)
		if err == nil {
			t.Fatalf("invalid, auth-domain or disabled user %d was accepted", user)
		}
	}
	requireCount(t, s, 1, 101, 0)
	for _, target := range []struct {
		typ  int
		id   int64
		hide string
	}{
		{1, 101, "UPDATE tb_article SET status=2 WHERE id=101"},
		{2, 201, "UPDATE tb_comment SET is_review=0 WHERE id=201"},
		{3, 301, "UPDATE tb_talk SET status=2 WHERE id=301"},
	} {
		// The comment's topic must be visible before its initial like.
		execSQL(t, db, "UPDATE tb_article SET status=1 WHERE id=101")
		requireToggle(t, s, 11, target.typ, target.id, true, 1)
		execSQL(t, db, target.hide)
		_, _, err := s.Toggle(ctx, 11, target.typ, target.id)
		requireCode(t, err, platform.CodeMissing)
		requireCount(t, s, target.typ, target.id, 1)
	}
}

func TestConcurrentLikesHaveCommittedOddEvenParity(t *testing.T) {
	_, cfg, first := fixture(t)
	second := engagement.New(openDB(t, cfg), cfg, func() time.Time { return fixedTime })
	ctx := context.Background()
	for _, batch := range []struct {
		n    int
		want int64
	}{{21, 1}, {20, 1}, {1, 0}} {
		start := make(chan struct{})
		failures := make(chan error, batch.n)
		var workers sync.WaitGroup
		for i := 0; i < batch.n; i++ {
			workers.Add(1)
			go func(i int) {
				defer workers.Done()
				<-start
				s := []*engagement.Service{first, second}[i%2]
				liked, count, err := s.Toggle(ctx, 11, 1, 101)
				if err != nil || (liked && count != 1) || (!liked && count != 0) {
					failures <- fmt.Errorf("committed result=(%v,%d,%v)", liked, count, err)
				}
			}(i)
		}
		close(start)
		workers.Wait()
		close(failures)
		for err := range failures {
			t.Error(err)
		}
		requireCount(t, first, 1, 101, batch.want)
		requireCount(t, second, 1, 101, batch.want)
	}
}

func TestViewsHashBrowserSessionsAndPersistFactsAfterReopen(t *testing.T) {
	db, cfg, s := fixture(t)
	otherDB := openDB(t, cfg)
	other := engagement.New(otherDB, cfg, func() time.Time { return fixedTime })
	ctx := context.Background()
	const cookie = "random-browser-session-cookie"
	var workers sync.WaitGroup
	for i := 0; i < 24; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			service := []*engagement.Service{s, other}[i%2]
			count, err := service.RecordView(ctx, 101, cookie)
			if err != nil || count != 1 {
				t.Errorf("same session view=%d %v, want 1", count, err)
			}
		}(i)
	}
	workers.Wait()
	if count, err := s.RecordView(ctx, 101, "another-browser-at-the-same-ip"); err != nil || count != 2 {
		t.Fatalf("independent browser view=%d %v", count, err)
	}
	if count, err := s.RecordView(ctx, 102, cookie); err != nil || count != 1 {
		t.Fatalf("same session another article=%d %v", count, err)
	}
	hash := sha256.Sum256([]byte(cookie))
	var stamp string
	if err := db.QueryRow("SELECT create_time FROM tb_article_view WHERE article_id=101 AND visitor_key=?", hex.EncodeToString(hash[:])).Scan(&stamp); err != nil || stamp != "2026-09-12T10:03:04" {
		t.Fatalf("hashed visitor/time missing: %s %v", stamp, err)
	}
	var rawCookies int
	if err := db.QueryRow("SELECT count(*) FROM tb_article_view WHERE visitor_key=?", cookie).Scan(&rawCookies); err != nil || rawCookies != 0 {
		t.Fatalf("raw cookie persisted: %d %v", rawCookies, err)
	}
	requireToggle(t, s, 11, 1, 101, true, 1)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := otherDB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := engagement.New(openDB(t, cfg), cfg, func() time.Time { return fixedTime })
	requireCount(t, reopened, 1, 101, 1)
	sets, err := reopened.UserSets(ctx, 11)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(sets["articleLikeSet"])
	if string(encoded) != "[101]" {
		t.Fatalf("likes lost after reopen: %s", encoded)
	}
	if count, err := reopened.RecordView(ctx, 101, cookie); err != nil || count != 2 {
		t.Fatalf("same cookie after reopen=%d %v", count, err)
	}
	if count, err := reopened.RecordView(ctx, 101, "new-browser-session"); err != nil || count != 3 {
		t.Fatalf("new browser session=%d %v", count, err)
	}
}

func TestEngagementDefaultsAndCommitFailureRollback(t *testing.T) {
	db, _, _ := fixture(t)
	s := engagement.New(db, platform.Config{}, nil)
	before := time.Now().UTC().Truncate(time.Second)
	requireToggle(t, s, 11, 1, 101, true, 1)
	var stamp string
	if err := db.QueryRow("SELECT create_time FROM tb_like").Scan(&stamp); err != nil {
		t.Fatal(err)
	}
	parsed, err := time.Parse("2006-01-02T15:04:05", stamp)
	if err != nil || parsed.Before(before) || parsed.After(time.Now().UTC()) {
		t.Fatalf("default clock/timezone stamp=%s %v", stamp, err)
	}
	for _, table := range []string{"tb_like", "tb_article_view"} {
		// The owning article FK is deferred, so the fact insert and count can
		// succeed while commit fails. Neither the fact nor the trigger may persist.
		execSQL(t, db, "CREATE TRIGGER reject_commit AFTER INSERT ON "+table+` BEGIN
			INSERT INTO tb_article(id,user_id,article_title,article_content,create_time)
			VALUES(999,999,'invalid owner','body','2026-09-12T10:00:00'); END`)
		if table == "tb_like" {
			_, _, err = s.Toggle(context.Background(), 11, 1, 102)
		} else {
			_, err = s.RecordView(context.Background(), 102, "browser")
		}
		if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
			t.Fatalf("%s commit failure was masked: %v", table, err)
		}
		execSQL(t, db, "DROP TRIGGER reject_commit")
		var partial int
		if err := db.QueryRow("SELECT count(*) FROM tb_article WHERE id=999").Scan(&partial); err != nil || partial != 0 {
			t.Fatalf("failed transaction persisted invalid object: %d %v", partial, err)
		}
	}
	requireCount(t, s, 1, 102, 0)
	var viewFacts int
	if err := db.QueryRow("SELECT count(*) FROM tb_article_view").Scan(&viewFacts); err != nil || viewFacts != 0 {
		t.Fatalf("failed commit persisted view fact: %d %v", viewFacts, err)
	}
	if count, err := s.RecordView(context.Background(), 102, "browser"); err != nil || count != 1 {
		t.Fatalf("commit failure persisted view or lock: %d %v", count, err)
	}
}

func TestViewsRejectUnavailableArticlesAndInvalidInput(t *testing.T) {
	db, _, s := fixture(t)
	ctx := context.Background()
	for _, id := range []int64{103, 104, 105, 999} {
		_, err := s.RecordView(ctx, id, "browser")
		requireCode(t, err, platform.CodeMissing)
	}
	for _, args := range []struct {
		id     int64
		cookie string
	}{{0, "browser"}, {-1, "browser"}, {101, ""}} {
		_, err := s.RecordView(ctx, args.id, args.cookie)
		requireCode(t, err, platform.CodeInvalid)
	}
	if _, err := s.RecordView(ctx, 101, "browser"); err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, "UPDATE tb_article SET status=2 WHERE id=101")
	_, err := s.RecordView(ctx, 101, "browser")
	requireCode(t, err, platform.CodeMissing)
	_, err = s.RecordView(ctx, 101, "new-browser")
	requireCode(t, err, platform.CodeMissing)
	var count int
	if err := db.QueryRow("SELECT count(*) FROM tb_article_view").Scan(&count); err != nil || count != 1 {
		t.Fatalf("invalid views wrote facts: %d %v", count, err)
	}
}

func TestDeleteTargetsUsesCallerTransactionAndKeepsOtherFacts(t *testing.T) {
	db, _, s := fixture(t)
	ctx := context.Background()
	execSQL(t, db, `INSERT INTO tb_comment(id,user_id,comment_content,type,create_time)
		VALUES(101,11,'same numeric ID as article',2,'2026-09-12T10:00:00')`)
	requireToggle(t, s, 11, 1, 101, true, 1)
	requireToggle(t, s, 22, 1, 101, true, 2)
	requireToggle(t, s, 11, 1, 102, true, 1)
	requireToggle(t, s, 11, 2, 201, true, 1)
	requireToggle(t, s, 11, 2, 101, true, 1)
	requireToggle(t, s, 11, 3, 301, true, 1)
	for _, commit := range []bool{false, true} {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteTargetsTx(ctx, tx, 1, nil); err != nil {
			t.Fatal(err)
		}
		ids := []int64{101, 102, 101}
		// More than SQLite's traditional 999 bound-variable limit.
		for id := int64(1000); id < 2200; id++ {
			ids = append(ids, id)
		}
		for repeat := 0; repeat < 2; repeat++ {
			if err := s.DeleteTargetsTx(ctx, tx, 1, ids); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM tb_article WHERE id IN (101,102)"); err != nil {
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		want := int64(2)
		if commit {
			want = 0
		}
		requireCount(t, s, 1, 101, want)
		if commit {
			requireCount(t, s, 1, 102, 0)
		} else {
			requireCount(t, s, 1, 102, 1)
		}
		requireCount(t, s, 2, 101, 1)
		requireCount(t, s, 2, 201, 1)
		requireCount(t, s, 3, 301, 1)
	}
}

func TestEngagementPreservesCancellationDatabaseErrorsAndAtomicity(t *testing.T) {
	db, _, s := fixture(t)
	ctx := context.Background()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err := s.Toggle(canceled, 11, 1, 101)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("toggle lost cancellation: %v", err)
	}
	if _, err := s.Count(canceled, 1, 101); !errors.Is(err, context.Canceled) {
		t.Fatalf("count lost cancellation: %v", err)
	}
	if _, err := s.UserSets(canceled, 11); !errors.Is(err, context.Canceled) {
		t.Fatalf("sets lost cancellation: %v", err)
	}
	if _, err := s.RecordView(canceled, 101, "browser"); !errors.Is(err, context.Canceled) {
		t.Fatalf("view lost cancellation: %v", err)
	}
	for _, typ := range []int{0, 4} {
		_, _, err := s.Toggle(ctx, 11, typ, 101)
		requireCode(t, err, platform.CodeInvalid)
		_, err = s.Count(ctx, typ, 101)
		requireCode(t, err, platform.CodeInvalid)
	}
	_, _, err = s.Toggle(ctx, 11, 1, 0)
	requireCode(t, err, platform.CodeInvalid)
	_, err = s.Count(ctx, 1, 0)
	requireCode(t, err, platform.CodeInvalid)
	_, err = s.UserSets(ctx, 0)
	requireCode(t, err, platform.CodeInvalid)
	execSQL(t, db, `CREATE TRIGGER reject_like BEFORE INSERT ON tb_like BEGIN SELECT RAISE(ABORT,'like write rejected'); END`)
	_, _, err = s.Toggle(ctx, 11, 1, 101)
	if err == nil || !strings.Contains(err.Error(), "like write rejected") {
		t.Fatalf("lost insert failure: %v", err)
	}
	requireCount(t, s, 1, 101, 0)
	execSQL(t, db, "DROP TRIGGER reject_like")
	requireToggle(t, s, 11, 1, 101, true, 1)
	execSQL(t, db, `CREATE TRIGGER reject_unlike BEFORE DELETE ON tb_like BEGIN SELECT RAISE(ABORT,'unlike rejected'); END`)
	_, _, err = s.Toggle(ctx, 11, 1, 101)
	if err == nil || !strings.Contains(err.Error(), "unlike rejected") {
		t.Fatalf("lost cancellation write failure: %v", err)
	}
	requireCount(t, s, 1, 101, 1)
	execSQL(t, db, "DROP TRIGGER reject_unlike")
	execSQL(t, db, `CREATE TRIGGER reject_view BEFORE INSERT ON tb_article_view BEGIN SELECT RAISE(ABORT,'view write rejected'); END`)
	if _, err := s.RecordView(ctx, 101, "browser"); err == nil || !strings.Contains(err.Error(), "view write rejected") {
		t.Fatalf("view masked failure: %v", err)
	}
	execSQL(t, db, "DROP TRIGGER reject_view")
	if count, err := s.RecordView(ctx, 101, "browser"); err != nil || count != 1 {
		t.Fatalf("failed view wrote a fact or retained a lock: %d %v", count, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireCode(t, s.DeleteTargetsTx(ctx, nil, 1, []int64{101}), platform.CodeInvalid)
	requireCode(t, s.DeleteTargetsTx(ctx, tx, 4, []int64{101}), platform.CodeInvalid)
	requireCode(t, s.DeleteTargetsTx(ctx, tx, 1, []int64{101, -1}), platform.CodeInvalid)
	if err := s.DeleteTargetsTx(canceled, tx, 1, []int64{101}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup lost cancellation: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	requireCount(t, s, 1, 101, 1)
	if err := s.DeleteTargetsTx(ctx, tx, 1, []int64{101}); !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("cleanup lost transaction state: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Count(ctx, 1, 101); err == nil {
		t.Fatal("closed database returned fake count")
	}
	if _, err := s.UserSets(ctx, 11); err == nil {
		t.Fatal("closed database returned fake sets")
	}
	if _, _, err := s.Toggle(ctx, 11, 1, 101); err == nil {
		t.Fatal("closed database returned toggle success")
	}
	if _, err := s.RecordView(ctx, 101, "browser"); err == nil {
		t.Fatal("closed database returned view success")
	}
}
