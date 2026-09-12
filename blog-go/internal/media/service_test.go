package media_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

func setup(t *testing.T) (*media.Service, *sql.DB, platform.Config) {
	t.Helper()
	cfg := platform.Config{DataDir: t.TempDir(), PublicURL: "https://example.com", TimeZone: time.FixedZone("Asia/Shanghai", 8*60*60)}
	db, err := platform.OpenSQLite(cfg)
	must(t, err)
	t.Cleanup(func() { db.Close() })
	must(t, platform.Migrate(context.Background(), db, []string{identity.Schema, catalog.Schema, article.Schema, media.Schema, community.Schema, engagement.Schema}, nil))
	_, err = db.Exec("INSERT INTO tb_user_info(id,nickname,avatar,create_time) VALUES(71,'Author','https://example.com/a.png','2026-09-12T00:00:00'),(72,'Other','https://example.com/b.png','2026-09-12T00:00:00')")
	must(t, err)
	now := func() time.Time { return time.Date(2026, 9, 12, 7, 8, 9, 0, time.UTC) }
	return media.New(db, cfg, engagement.New(db, cfg, now), now), db, cfg
}
func input(value any) platform.Input {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return platform.Input{Body: body, Actor: platform.Actor{LoggedIn: true, AuthID: 999, UserInfoID: 71}, Query: make(url.Values)}
}
func parameter(name string, id int64) platform.Input {
	return platform.Input{Params: map[string]string{name: fmt.Sprint(id)}, Actor: platform.Actor{LoggedIn: true, AuthID: 999, UserInfoID: 71}, Query: make(url.Values)}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func number(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	must(t, db.QueryRow(query, args...).Scan(&n))
	return n
}
func wantError(t *testing.T, err error, code int) {
	t.Helper()
	var b *platform.Error
	if !errors.As(err, &b) || b.Code != code {
		t.Fatalf("error %v, want code %d", err, code)
	}
}
func album(t *testing.T, s *media.Service, db *sql.DB, name string, status int) int64 {
	t.Helper()
	result, err := s.SaveOrUpdatePhotoAlbum(context.Background(), input(platform.Object{"albumName": name, "albumDesc": "description", "albumCover": "https://example.com/cover.png", "status": status}))
	must(t, err)
	if result != nil {
		t.Fatalf("save album changed nil result: %v", result)
	}
	return number(t, db, "SELECT id FROM tb_photo_album WHERE album_name=?", name)
}
func photos(t *testing.T, s *media.Service, db *sql.DB, id int64) []int64 {
	t.Helper()
	_, err := s.SavePhotos(context.Background(), input(platform.Object{"albumId": id, "photoUrlList": []string{"https://example.com/one.png", "https://example.com/two.png"}}))
	must(t, err)
	rows, err := db.Query("SELECT id FROM tb_photo WHERE album_id=? ORDER BY id", id)
	must(t, err)
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		must(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	must(t, rows.Err())
	return ids
}
func talk(t *testing.T, s *media.Service, db *sql.DB, content string, status, top int) int64 {
	t.Helper()
	value, err := s.SaveOrUpdateTalk(context.Background(), input(platform.Object{"content": content, "images": nil, "status": status, "isTop": top}))
	must(t, err)
	if value != nil {
		t.Fatal("save talk result changed")
	}
	return number(t, db, "SELECT id FROM tb_talk WHERE content=?", content)
}

func TestAlbumPhotoLifecycleAndOriginalProjections(t *testing.T) {
	s, db, cfg := setup(t)
	ctx := context.Background()
	id := album(t, s, db, "Private", 2)
	ids := photos(t, s, db, id)
	value, err := s.GetPhotoAlbumBackById(ctx, parameter("albumId", id))
	must(t, err)
	back := value.(platform.Object)
	if len(back) != 6 || back["albumName"] != "Private" || back["photoCount"] != int64(2) || back["status"] != int64(2) {
		t.Fatalf("album projection: %v", back)
	}
	value, err = s.ListPhotoAlbumBacks(ctx, platform.Input{Query: url.Values{"keywords": {"riva"}, "size": {"1"}}})
	must(t, err)
	page := value.(platform.Page)
	if page.Count != 1 || len(page.RecordList) != 1 {
		t.Fatalf("album page: %+v", page)
	}
	value, err = s.ListPhotoAlbumBackInfos(ctx, platform.Input{})
	must(t, err)
	if list := value.([]platform.Object); len(list) != 1 || len(list[0]) != 4 {
		t.Fatalf("album selector projection: %v", value)
	}
	_, err = s.ListPhotosByAlbumId(ctx, parameter("albumId", id))
	wantError(t, err, platform.CodeFail)
	_, err = s.UpdatePhoto(ctx, input(platform.Object{"id": ids[0], "photoName": "Named", "photoDesc": "Caption"}))
	must(t, err)
	value, err = s.ListPhotos(ctx, platform.Input{Query: url.Values{"albumId": {fmt.Sprint(id)}, "isDelete": {"0"}}})
	must(t, err)
	page = value.(platform.Page)
	if page.Count != 2 || len(page.RecordList[0]) != 4 || page.RecordList[1]["photoName"] != "Named" {
		t.Fatalf("photo page: %+v", page)
	}
	_, err = s.DeletePhotoAlbumById(ctx, parameter("albumId", id))
	must(t, err)
	if number(t, db, "SELECT is_delete FROM tb_photo_album WHERE id=?", id) != 1 || number(t, db, "SELECT count(*) FROM tb_photo WHERE album_id=? AND is_delete=1", id) != 2 {
		t.Fatal("album and photos not trashed together")
	}
	value, err = s.ListPhotoAlbumBackInfos(ctx, platform.Input{})
	must(t, err)
	if len(value.([]platform.Object)) != 0 {
		t.Fatal("deleted album remained selectable")
	}
	_, err = s.UpdatePhotoDelete(ctx, input(platform.Object{"idList": []int64{ids[0]}, "isDelete": 0}))
	must(t, err)
	if number(t, db, "SELECT is_delete FROM tb_photo_album WHERE id=?", id) != 0 || number(t, db, "SELECT status FROM tb_photo_album WHERE id=?", id) != 2 {
		t.Fatal("restore altered album privacy")
	}
	_, err = s.ListPhotosByAlbumId(ctx, parameter("albumId", id))
	wantError(t, err, platform.CodeFail)
	_, err = s.SaveOrUpdatePhotoAlbum(ctx, input(platform.Object{"id": id, "albumName": "Public", "albumCover": "https://example.com/new.png", "albumDesc": "new desc", "status": 1}))
	must(t, err)
	value, err = s.ListPhotosByAlbumId(ctx, parameter("albumId", id))
	must(t, err)
	public := value.(platform.Object)
	if len(public) != 3 || public["photoAlbumName"] != "Public" || len(public["photoList"].([]string)) != 1 || public["photoList"].([]string)[0] != "https://example.com/one.png" {
		t.Fatalf("public photos: %v", public)
	}
	dest := album(t, s, db, "Destination", 1)
	_, err = s.UpdatePhotosAlbum(ctx, input(platform.Object{"albumId": dest, "photoIdList": []int64{ids[0]}}))
	must(t, err)
	if number(t, db, "SELECT album_id FROM tb_photo WHERE id=?", ids[0]) != dest {
		t.Fatal("move missing")
	}
	file := filepath.Join(cfg.DataDir, "files", "photos", "shared.png")
	must(t, os.MkdirAll(filepath.Dir(file), 0700))
	must(t, os.WriteFile(file, []byte("shared"), 0600))
	sharedURL := cfg.PublicURL + "/files/photos/shared.png"
	_, err = db.Exec("UPDATE tb_photo SET photo_src=? WHERE id IN (?,?)", sharedURL, ids[0], ids[1])
	must(t, err)
	_, err = s.SaveOrUpdateTalk(ctx, input(platform.Object{"content": "References the same photo", "images": fmt.Sprintf("[%q]", sharedURL), "isTop": 0, "status": 1}))
	must(t, err)
	_, err = s.DeletePhotos(ctx, input(ids))
	must(t, err)
	if number(t, db, "SELECT count(*) FROM tb_photo") != 0 {
		t.Fatal("permanent deletion missing")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("shared file removed", err)
	}
	_, err = s.DeletePhotoAlbumById(ctx, parameter("albumId", id))
	must(t, err)
	if number(t, db, "SELECT count(*) FROM tb_photo_album WHERE id=?", id) != 0 {
		t.Fatal("empty album not physically deleted")
	}
	must(t, db.Close())
	reopened, err := platform.OpenSQLite(cfg)
	must(t, err)
	defer reopened.Close()
	restarted := media.New(reopened, cfg, engagement.New(reopened, cfg, nil), nil)
	value, err = restarted.ListPhotoAlbums(ctx, platform.Input{})
	must(t, err)
	if list := value.([]platform.Object); len(list) != 1 || list[0]["albumName"] != "Destination" {
		t.Fatalf("restart lost state: %v", value)
	}
}
func TestPhotoBatchesValidateAllIDsAndRollback(t *testing.T) {
	s, db, _ := setup(t)
	ctx := context.Background()
	a := album(t, s, db, "A", 1)
	b := album(t, s, db, "B", 1)
	ids := photos(t, s, db, a)
	for _, call := range []struct {
		fn   platform.Endpoint
		body any
	}{
		{s.UpdatePhotosAlbum, platform.Object{"albumId": b, "photoIdList": []int64{ids[0], 99999}}},
		{s.UpdatePhotoDelete, platform.Object{"idList": []int64{ids[0], 99999}, "isDelete": 1}},
		{s.DeletePhotos, []int64{ids[0], 99999}},
	} {
		_, err := call.fn(ctx, input(call.body))
		wantError(t, err, platform.CodeFail)
		if number(t, db, "SELECT count(*) FROM tb_photo WHERE album_id=? AND is_delete=0", a) != 2 {
			t.Fatal("invalid batch partially changed photos")
		}
	}
	_, err := s.DeletePhotoAlbumById(ctx, parameter("albumId", a))
	must(t, err)
	for _, call := range []struct {
		fn   platform.Endpoint
		body any
	}{
		{s.SavePhotos, platform.Object{"albumId": a, "photoUrlList": []string{"https://example.com/a.png"}}},
		{s.UpdatePhotosAlbum, platform.Object{"albumId": a, "photoIdList": ids}},
	} {
		_, err := call.fn(ctx, input(call.body))
		wantError(t, err, platform.CodeFail)
	}
	_, err = db.Exec(fmt.Sprintf("CREATE TRIGGER fail_restore BEFORE UPDATE OF is_delete ON tb_photo WHEN NEW.id=%d AND NEW.is_delete=0 BEGIN SELECT RAISE(ABORT,'restore failure'); END", ids[1]))
	must(t, err)
	_, err = s.UpdatePhotoDelete(ctx, input(platform.Object{"idList": ids, "isDelete": 0}))
	if err == nil {
		t.Fatal("injected restore failure hidden")
	}
	if number(t, db, "SELECT count(*) FROM tb_photo WHERE is_delete=1") != 2 || number(t, db, "SELECT is_delete FROM tb_photo_album WHERE id=?", a) != 1 {
		t.Fatal("restore failed to roll back album/photos")
	}
	_, err = db.Exec("DROP TRIGGER fail_restore")
	must(t, err)
	_, err = s.UpdatePhotoDelete(ctx, input(platform.Object{"idList": ids, "isDelete": 0}))
	must(t, err)
	_, err = db.Exec(fmt.Sprintf("CREATE TRIGGER fail_album_delete BEFORE UPDATE OF is_delete ON tb_photo WHEN NEW.id=%d BEGIN SELECT RAISE(ABORT,'delete failure'); END", ids[1]))
	must(t, err)
	_, err = s.DeletePhotoAlbumById(ctx, parameter("albumId", a))
	if err == nil {
		t.Fatal("injected deletion failure hidden")
	}
	if number(t, db, "SELECT is_delete FROM tb_photo_album WHERE id=?", a) != 0 || number(t, db, "SELECT count(*) FROM tb_photo WHERE is_delete=0") != 2 {
		t.Fatal("album deletion not atomic")
	}
	_, err = db.Exec("DROP TRIGGER fail_album_delete")
	must(t, err)
	_, err = db.Exec("CREATE TRIGGER fail_second_insert BEFORE INSERT ON tb_photo WHEN NEW.photo_src LIKE '%two.png' BEGIN SELECT RAISE(ABORT,'insert failure'); END")
	must(t, err)
	_, err = s.SavePhotos(ctx, input(platform.Object{"albumId": b, "photoUrlList": []string{"https://example.com/new.png", "https://example.com/two.png"}}))
	if err == nil {
		t.Fatal("partial insert failure hidden")
	}
	if number(t, db, "SELECT count(*) FROM tb_photo WHERE album_id=?", b) != 0 {
		t.Fatal("batch save partially committed")
	}
}
func TestTalkVisibilityImagesCountsLikesAndOwnership(t *testing.T) {
	s, db, _ := setup(t)
	ctx := context.Background()
	visible := talk(t, s, db, "<p>Public</p>", 1, 0)
	hidden := talk(t, s, db, "Hidden", 2, 1)
	top := talk(t, s, db, "Pinned", 1, 1)
	rawImages := " [\"https://example.com/one.png\", \"https://example.com/two.png\"] "
	_, err := s.SaveOrUpdateTalk(ctx, input(platform.Object{"id": visible, "content": "<p>Public</p>", "images": rawImages, "isTop": 0, "status": 1}))
	must(t, err)
	if number(t, db, "SELECT user_id FROM tb_talk WHERE id=?", visible) != 71 {
		t.Fatal("AuthID used instead of user_info ID")
	}
	_, err = db.Exec("INSERT INTO tb_comment(id,user_id,topic_id,comment_content,type,is_review,is_delete,parent_id,create_time) VALUES(1,71,?,'root',3,1,0,NULL,'2026-01-01'),(2,71,?,'reply',3,1,0,1,'2026-01-01'),(3,71,?,'pending',3,0,0,NULL,'2026-01-01'),(4,71,?,'deleted',3,1,1,NULL,'2026-01-01'),(5,71,?,'article',1,1,0,NULL,'2026-01-01')", visible, visible, visible, visible, visible)
	must(t, err)
	value, err := s.ListTalks(ctx, platform.Input{})
	must(t, err)
	page := value.(platform.Page)
	if page.Count != 2 || len(page.RecordList) != 2 || page.RecordList[0]["id"] != top {
		t.Fatalf("public order/filter: %+v", page)
	}
	dto := page.RecordList[1]
	if len(dto) != 10 || dto["images"] != rawImages || len(dto["imgList"].([]string)) != 2 || dto["commentCount"] != int64(1) || dto["likeCount"] != int64(0) || dto["createTime"] != "2026-09-12T15:08:09" {
		t.Fatalf("talk projection: %v", dto)
	}
	value, err = s.GetTalkById(ctx, parameter("talkId", visible))
	must(t, err)
	if value.(platform.Object)["commentCount"] != int64(1) {
		t.Fatal("detail comment count inconsistent")
	}
	_, err = s.GetTalkById(ctx, parameter("talkId", hidden))
	wantError(t, err, platform.CodeFail)
	value, err = s.GetBackTalkById(ctx, parameter("talkId", hidden))
	must(t, err)
	back := value.(platform.Object)
	if len(back) != 9 || back["status"] != int64(2) || back["images"] != nil || back["imgList"] == nil {
		t.Fatalf("admin projection: %v", back)
	}
	for i := 0; i < 2; i++ {
		result, err := s.SaveTalkLike(ctx, parameter("talkId", visible))
		must(t, err)
		if result != nil {
			t.Fatal("like result changed")
		}
		if number(t, db, "SELECT count(*) FROM tb_like WHERE user_info_id=71 AND target_type=3 AND target_id=?", visible) != int64(1-i) {
			t.Fatal("like not toggled in profile domain")
		}
	}
	_, err = s.SaveTalkLike(ctx, parameter("talkId", hidden))
	wantError(t, err, platform.CodeFail)
	if !containsErrorCode(err, platform.CodeMissing) {
		t.Fatal("wire mapping discarded the engagement error cause")
	}
	value, err = s.ListBackTalks(ctx, platform.Input{Query: url.Values{"status": {"2"}}})
	must(t, err)
	if page := value.(platform.Page); page.Count != 1 || page.RecordList[0]["id"] != hidden {
		t.Fatalf("admin status filter: %+v", page)
	}
	edited := input(platform.Object{"id": visible, "content": "edited", "images": nil, "isTop": 0, "status": 1})
	edited.Actor.UserInfoID = 72
	_, err = s.SaveOrUpdateTalk(ctx, edited)
	must(t, err)
	if number(t, db, "SELECT user_id FROM tb_talk WHERE id=?", visible) != 72 {
		t.Fatal("write did not use current profile")
	}
}
func TestTalkDeletionCleansCommentsAndLikesAtomically(t *testing.T) {
	s, db, cfg := setup(t)
	ctx := context.Background()
	id := talk(t, s, db, "remove", 1, 0)
	other := talk(t, s, db, "keep", 1, 0)
	_, err := db.Exec("INSERT INTO tb_comment(id,user_id,topic_id,comment_content,type,is_review,create_time) VALUES(1,71,?,'root',3,1,'2026-01-01'),(3,71,?,'other type',1,1,'2026-01-01')", id, id)
	must(t, err)
	_, err = db.Exec("INSERT INTO tb_comment(id,user_id,topic_id,comment_content,type,is_review,parent_id,create_time) VALUES(2,71,?,'reply',3,1,1,'2026-01-01')", id)
	must(t, err)
	_, err = db.Exec("INSERT INTO tb_like(user_info_id,target_type,target_id,create_time) VALUES(71,3,?,'2026-01-01'),(71,3,?,'2026-01-01'),(71,2,1,'2026-01-01'),(72,2,2,'2026-01-01'),(71,2,3,'2026-01-01')", id, other)
	must(t, err)
	_, err = s.DeleteTalks(ctx, input([]int64{id, 99999}))
	wantError(t, err, platform.CodeFail)
	if number(t, db, "SELECT count(*) FROM tb_like") != 5 {
		t.Fatal("invalid talk batch cleared likes")
	}
	_, err = db.Exec(fmt.Sprintf("CREATE TRIGGER block_talk_delete BEFORE DELETE ON tb_talk WHEN OLD.id=%d BEGIN SELECT RAISE(ABORT,'failure'); END", id))
	must(t, err)
	_, err = s.DeleteTalks(ctx, input([]int64{id}))
	if err == nil {
		t.Fatal("injected talk deletion failure hidden")
	}
	if number(t, db, "SELECT count(*) FROM tb_comment") != 3 || number(t, db, "SELECT count(*) FROM tb_like") != 5 {
		t.Fatal("related deletion was not rolled back")
	}
	_, err = db.Exec("DROP TRIGGER block_talk_delete")
	must(t, err)
	sentinel := filepath.Join(cfg.DataDir, "files", "talks", "keep-file.png")
	must(t, os.MkdirAll(filepath.Dir(sentinel), 0700))
	must(t, os.WriteFile(sentinel, []byte("shared"), 0600))
	sharedURL := cfg.PublicURL + "/files/talks/keep-file.png"
	_, err = db.Exec("UPDATE tb_talk SET images=? WHERE id IN (?,?)", fmt.Sprintf("[%q]", sharedURL), id, other)
	must(t, err)
	_, err = s.DeleteTalks(ctx, input([]int64{id, id}))
	must(t, err)
	if number(t, db, "SELECT count(*) FROM tb_talk") != 1 || number(t, db, "SELECT count(*) FROM tb_comment") != 1 || number(t, db, "SELECT count(*) FROM tb_like") != 2 {
		t.Fatal("delete scope or associated cleanup incorrect")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("shared file removed", err)
	}
}
func TestMediaValidationPaginationAndErrors(t *testing.T) {
	s, db, _ := setup(t)
	ctx := context.Background()
	for _, call := range []struct {
		fn   platform.Endpoint
		body any
	}{
		{s.SaveOrUpdatePhotoAlbum, platform.Object{"albumName": " ", "albumCover": "https://example.com/a.png"}},
		{s.SaveOrUpdatePhotoAlbum, platform.Object{"albumName": "A", "albumCover": "javascript:alert(1)"}},
		{s.SaveOrUpdatePhotoAlbum, platform.Object{"albumName": "A", "albumCover": "https://example.com/a.png", "status": 3}},
		{s.SaveOrUpdateTalk, platform.Object{"content": " ", "status": 1, "isTop": 0}},
		{s.SaveOrUpdateTalk, platform.Object{"content": "test", "status": 1, "isTop": 9}},
		{s.SaveOrUpdateTalk, platform.Object{"content": "test", "status": 1, "isTop": 0, "images": "[1]"}},
		{s.SaveOrUpdateTalk, platform.Object{"content": "test", "status": 1, "isTop": 0, "images": "{\"a\":1}"}},
		{s.SaveOrUpdateTalk, platform.Object{"content": "test", "status": 1, "isTop": 0, "images": "[\"javascript:bad\"]"}},
		{s.SavePhotos, platform.Object{"albumId": 1, "photoUrlList": []string{}}},
		{s.UpdatePhotoDelete, platform.Object{"idList": []int64{1}, "isDelete": 2}},
		{s.DeletePhotos, []int64{}}, {s.DeleteTalks, []int64{0}}, {s.UpdatePhoto, platform.Object{"id": 1, "photoName": ""}},
	} {
		_, err := call.fn(ctx, input(call.body))
		wantError(t, err, platform.CodeInvalid)
	}
	if number(t, db, "SELECT count(*) FROM tb_photo_album") != 0 || number(t, db, "SELECT count(*) FROM tb_talk") != 0 {
		t.Fatal("validation left state")
	}
	for _, fn := range []platform.Endpoint{s.ListTalks, s.ListBackTalks, s.ListPhotoAlbumBacks, s.ListPhotos} {
		value, err := fn(ctx, platform.Input{})
		must(t, err)
		page := value.(platform.Page)
		if page.Count != 0 || page.RecordList == nil {
			t.Fatal("empty page not normalized")
		}
		_, err = fn(ctx, platform.Input{Query: url.Values{"size": {"101"}}})
		wantError(t, err, platform.CodeInvalid)
	}
	id := album(t, s, db, "Unique", 2)
	_, err := s.SaveOrUpdatePhotoAlbum(ctx, input(platform.Object{"albumName": "unique", "albumCover": "https://example.com/a.png"}))
	wantError(t, err, platform.CodeFail)
	_, err = s.SaveOrUpdatePhotoAlbum(ctx, input(platform.Object{"id": id, "albumName": "Unique", "albumCover": "https://example.com/a.png"}))
	must(t, err)
	if number(t, db, "SELECT status FROM tb_photo_album WHERE id=?", id) != 2 {
		t.Fatal("omitted status publicized private album")
	}
	var description string
	must(t, db.QueryRow("SELECT album_desc FROM tb_photo_album WHERE id=?", id).Scan(&description))
	if description != "description" {
		t.Fatal("omitted album description overwrote saved content")
	}
	value, err := s.ListPhotoAlbumBacks(ctx, platform.Input{Query: url.Values{"current": {"9223372036854775807"}, "size": {"100"}}})
	must(t, err)
	if page := value.(platform.Page); page.Count != 1 || len(page.RecordList) != 0 {
		t.Fatal("page overflow lost count")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.ListPhotos(canceled, platform.Input{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel identity lost: %v", err)
	}
	must(t, db.Close())
	_, err = s.ListPhotoAlbums(ctx, platform.Input{})
	if err == nil {
		t.Fatal("database failure returned a fake empty result")
	}
}
func TestMediaRoutesAndHomeTalks(t *testing.T) {
	s, db, _ := setup(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		talk(t, s, db, fmt.Sprintf("Talk %02d", i), 1, 0)
	}
	talk(t, s, db, "Secret", 2, 1)
	talk(t, s, db, "<script>bad()</script><p>Pinned</p>", 1, 1)
	value, err := s.ListHomeTalks(ctx, platform.Input{})
	must(t, err)
	home := value.([]string)
	if len(home) != 10 || home[0] != "<p>Pinned</p>" || home[1] != "Talk 11" {
		t.Fatalf("home talks order/format: %v", home)
	}
	routes := s.Routes()
	if len(routes) != 21 {
		t.Fatalf("route count %d", len(routes))
	}
	logged, allowed := true, true
	router, err := platform.NewRouter(routes, platform.Callbacks{Resolve: func(*http.Request) (platform.Actor, error) {
		return platform.Actor{LoggedIn: logged, UserInfoID: 71, AuthID: 999}, nil
	}, Authorize: func(context.Context, platform.Actor, string, string) error {
		if !allowed {
			return &platform.Error{Code: platform.CodeFail, Message: "denied"}
		}
		return nil
	}}, platform.Config{})
	must(t, err)
	request := func() platform.Result {
		req := httptest.NewRequest("POST", "/admin/photos/albums", strings.NewReader(`{"albumName":"HTTP","albumCover":"https://example.com/a.png","status":1}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var result platform.Result
		must(t, json.Unmarshal(w.Body.Bytes(), &result))
		return result
	}
	allowed = false
	if result := request(); result.Flag {
		t.Fatal("authorization bypass")
	}
	logged = false
	allowed = true
	if result := request(); result.Flag {
		t.Fatal("anonymous write")
	}
	logged = true
	if result := request(); !result.Flag || result.Data != nil {
		t.Fatalf("wire result: %+v", result)
	}
	if number(t, db, "SELECT count(*) FROM tb_photo_album") != 1 {
		t.Fatal("route binding/state incorrect")
	}
	if result := request(); result.Flag || result.Code != platform.CodeFail {
		t.Fatalf("ordinary duplicate returned an account code: %+v", result)
	}
}
func TestConcurrentAlbumCreationAndPhotoDeletion(t *testing.T) {
	s, db, cfg := setup(t)
	otherDB, err := platform.OpenSQLite(cfg)
	must(t, err)
	defer otherDB.Close()
	other := media.New(otherDB, cfg, engagement.New(otherDB, cfg, nil), nil)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, service := range []*media.Service{s, other} {
		wg.Add(1)
		go func(service *media.Service) {
			defer wg.Done()
			_, err := service.SaveOrUpdatePhotoAlbum(context.Background(), input(platform.Object{"albumName": "One", "albumCover": "https://example.com/a.png"}))
			errs <- err
		}(service)
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			wantError(t, err, platform.CodeFail)
		}
	}
	if success != 1 || number(t, db, "SELECT count(*) FROM tb_photo_album") != 1 {
		t.Fatal("duplicate name race")
	}
	id := number(t, db, "SELECT id FROM tb_photo_album")
	errs = make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := s.SavePhotos(context.Background(), input(platform.Object{"albumId": id, "photoUrlList": []string{"https://example.com/a.png"}}))
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := other.DeletePhotoAlbumById(context.Background(), parameter("albumId", id))
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			wantError(t, err, platform.CodeFail)
		}
	}
	if number(t, db, "SELECT count(*) FROM tb_photo p LEFT JOIN tb_photo_album a ON a.id=p.album_id WHERE a.id IS NULL OR (a.is_delete=1 AND p.is_delete=0)") != 0 {
		t.Fatal("concurrent add/delete broke ownership invariant")
	}
}

// Traverse standard Go error chains, regardless of whether a wrapper has one or several causes.
func containsErrorCode(err error, code int) bool {
	if err == nil {
		return false
	}
	if business, ok := err.(*platform.Error); ok && business.Code == code {
		return true
	}
	if multiple, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range multiple.Unwrap() {
			if containsErrorCode(cause, code) {
				return true
			}
		}
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		return containsErrorCode(single.Unwrap(), code)
	}
	return false
}
