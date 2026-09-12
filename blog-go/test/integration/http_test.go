package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"blog-go/internal/platform"
)

func TestAllBusinessRoutes(t *testing.T) {
	p := newProviders(t)
	f := newFixture(t, p.configure)
	t.Run("FR-009 website and pages", func(t *testing.T) {
		home := object(t, f.ok(t, f.guest, "GET", "/", nil))
		website := object(t, home["websiteConfig"])
		if len(website) != 24 || website["websocketUrl"] != "ws"+strings.TrimPrefix(f.cfg.PublicURL, "http")+"/websocket" {
			t.Fatal("initial website cannot configure the frontend")
		}
		f.ok(t, f.admin, "PUT", "/admin/website/config", map[string]any{"websiteName": "Integration website", "websiteNotice": "Verified locally"})
		cfg := object(t, f.ok(t, f.admin, "GET", "/admin/website/config", nil))
		if cfg["websiteName"] != "Integration website" {
			t.Fatal("website edit not returned")
		}
		f.ok(t, f.admin, "PUT", "/admin/about", map[string]any{"aboutContent": "# Integration about"})
		if f.ok(t, f.guest, "GET", "/about", nil) != "# Integration about" {
			t.Fatal("about edit not visible")
		}
		f.ok(t, f.admin, "POST", "/admin/pages", map[string]any{"pageName": "Integration page", "pageLabel": "integration-page", "pageCover": website["articleCover"]})
		id := rowID(t, f.app.DB, "SELECT id FROM tb_page WHERE page_label='integration-page'")
		found := false
		for _, value := range list(t, f.ok(t, f.admin, "GET", "/admin/pages", nil)) {
			if object(t, value)["id"] == float64(id) {
				found = true
			}
		}
		if !found {
			t.Fatal("saved page missing")
		}
		f.ok(t, f.admin, "DELETE", pathID("/admin/pages/%d", id), nil)
		if rowID(t, f.app.DB, "SELECT count(*) FROM tb_page WHERE id=?", id) != 0 {
			t.Fatal("page deletion missing")
		}
	})
	if t.Failed() {
		return
	}
	images := map[string]string{}
	t.Run("FR-004 FR-011 actual image uploads and profile", func(t *testing.T) {
		for _, route := range []string{"/admin/articles/images", "/admin/config/images", "/admin/photos/albums/cover", "/admin/talks/images"} {
			r := f.upload(t, f.admin, route, "sample.png", "image/png", imageData(t, false), nil)
			if !r.result.Flag {
				t.Fatalf("image upload failed: %#v", r.result)
			}
			location, ok := r.result.Data.(string)
			if !ok {
				t.Fatal("upload URL missing")
			}
			images[route] = location
			assertImage(t, f.guest, location, "image/png")
		}
		// The unchanged admin uploader can label compressed JPEG bytes as PNG.
		r := f.upload(t, f.user, "/users/avatar", "old-name.png", "image/png", imageData(t, true), nil)
		if !r.result.Flag {
			t.Fatalf("compressed avatar rejected: %#v", r.result)
		}
		avatar := r.result.Data.(string)
		if !strings.HasSuffix(avatar, ".jpg") {
			t.Fatal("actual JPEG type was not normalized")
		}
		assertImage(t, f.guest, avatar, "image/jpeg")
		f.ok(t, f.user, "PUT", "/users/info", map[string]any{"nickname": "Updated reader", "intro": "Integration bio", "webSite": "https://example.test", "userInfoId": 1})
		login := object(t, f.ok(t, f.user, "POST", "/login", url.Values{"username": {"reader@example.test"}, "password": {"fixture-password"}}))
		if login["nickname"] != "Updated reader" || login["avatar"] != avatar || login["userInfoId"] != float64(11) || login["id"] != float64(101) || login["password"] != nil {
			t.Fatal("profile/identity projection failed")
		}
		f.fail(t, f.guest, "POST", "/login", url.Values{"username": {"reader@example.test"}, "password": {"wrong-password"}}, platform.CodeFail)
	})
	if t.Failed() {
		return
	}
	var category, tag, link, articleID int64
	t.Run("FR-003 catalog lifecycle", func(t *testing.T) {
		f.ok(t, f.admin, "POST", "/admin/categories", map[string]any{"categoryName": "Category A"})
		category = rowID(t, f.app.DB, "SELECT id FROM tb_category WHERE category_name='Category A'")
		f.ok(t, f.admin, "POST", "/admin/categories", map[string]any{"id": category, "categoryName": "Category B"})
		f.ok(t, f.admin, "POST", "/admin/tags", map[string]any{"tagName": "Tag A"})
		tag = rowID(t, f.app.DB, "SELECT id FROM tb_tag WHERE tag_name='Tag A'")
		f.ok(t, f.admin, "POST", "/admin/tags", map[string]any{"id": tag, "tagName": "Tag B"})
		for _, kind := range []string{"categories", "tags"} {
			if page(t, f.ok(t, f.guest, "GET", "/"+kind, nil))["count"].(float64) < 2 {
				t.Fatal("public navigation missing catalog data")
			}
			if page(t, f.ok(t, f.admin, "GET", "/admin/"+kind+"?keywords=absent", nil))["count"] != float64(0) {
				t.Fatal("catalog empty page not empty")
			}
			if len(list(t, f.ok(t, f.admin, "GET", "/admin/"+kind+"/search?keywords="+map[string]string{"categories": "Category", "tags": "Tag"}[kind], nil))) != 1 {
				t.Fatal("article editor search did not match catalog")
			}
		}
		payload := map[string]any{"linkName": "Integration link", "linkIntro": "Initial link", "linkAvatar": images["/admin/config/images"], "linkAddress": f.cfg.PublicURL}
		f.ok(t, f.admin, "POST", "/admin/links", payload)
		link = rowID(t, f.app.DB, "SELECT id FROM tb_friend_link WHERE link_name='Integration link'")
		payload["id"] = link
		payload["linkIntro"] = "Edited link"
		f.ok(t, f.admin, "POST", "/admin/links", payload)
		if len(list(t, f.ok(t, f.guest, "GET", "/links", nil))) < 2 || page(t, f.ok(t, f.admin, "GET", "/admin/links?keywords=Integration", nil))["count"] != float64(1) {
			t.Fatal("friend link edit not readable")
		}
	})
	if t.Failed() {
		return
	}
	content := "# Integration article\n\nSaved Markdown.\n\n![image](" + images["/admin/articles/images"] + ")\n"
	article := map[string]any{"articleTitle": "Integration article", "articleContent": content, "articleCover": images["/admin/articles/images"], "categoryName": "Category B", "tagNameList": []string{"Tag B"}, "status": 3, "type": 1, "isTop": 0}
	t.Run("FR-001 FR-002 publish visibility reads and likes", func(t *testing.T) {
		f.ok(t, f.admin, "POST", "/admin/articles", article)
		articleID = rowID(t, f.app.DB, "SELECT id FROM tb_article WHERE article_title='Integration article'")
		article["id"] = articleID
		back := object(t, f.ok(t, f.admin, "GET", pathID("/admin/articles/%d", articleID), nil))
		if back["articleContent"] != content || back["categoryName"] != "Category B" {
			t.Fatal("draft editor did not round-trip content")
		}
		f.fail(t, f.guest, "GET", pathID("/articles/%d", articleID), nil, platform.CodeFail)
		article["status"] = 2
		f.ok(t, f.admin, "POST", "/admin/articles", article)
		f.fail(t, f.guest, "GET", pathID("/articles/%d", articleID), nil, platform.CodeFail)
		article["status"] = 1
		f.ok(t, f.admin, "POST", "/admin/articles", article)
		f.ok(t, f.admin, "PUT", "/admin/articles/top", map[string]any{"id": articleID, "isTop": 1})
		first := object(t, f.ok(t, f.guest, "GET", pathID("/articles/%d", articleID), nil))
		again := object(t, f.ok(t, f.guest, "GET", pathID("/articles/%d", articleID), nil))
		if first["articleContent"] != content || first["viewsCount"] != again["viewsCount"] {
			t.Fatal("detail body or visitor idempotence failed")
		}
		for _, want := range []float64{1, 0} {
			f.ok(t, f.user, "POST", pathID("/articles/%d/like", articleID), nil)
			detail := object(t, f.ok(t, f.guest, "GET", pathID("/articles/%d", articleID), nil))
			if detail["likeCount"] != want {
				t.Fatal("article like cancellation/count drift")
			}
		}
		if len(list(t, f.ok(t, f.guest, "GET", "/articles", nil))) < 2 || page(t, f.ok(t, f.guest, "GET", "/articles/archives", nil))["count"].(float64) < 2 {
			t.Fatal("public article lists missing published article")
		}
		if len(list(t, f.ok(t, f.guest, "GET", "/articles/search?keywords=Integration", nil))) != 1 || len(list(t, f.ok(t, f.guest, "GET", "/articles/search?keywords=absent", nil))) != 0 {
			t.Fatal("article search match/empty contract failed")
		}
		condition := object(t, f.ok(t, f.guest, "GET", pathID("/articles/condition?categoryId=%d", category), nil))
		if len(list(t, condition["articlePreviewDTOList"])) != 1 {
			t.Fatal("category article projection failed")
		}
		if page(t, f.ok(t, f.admin, "GET", "/admin/articles?keywords=Integration&status=1", nil))["count"] != float64(1) {
			t.Fatal("admin article filter failed")
		}
		for _, deleted := range []int{1, 0} {
			f.ok(t, f.admin, "PUT", "/admin/articles", map[string]any{"idList": []int64{articleID}, "isDelete": deleted})
			if rowID(t, f.app.DB, "SELECT is_top FROM tb_article WHERE id=?", articleID) != 0 {
				t.Fatal("trash retained pin")
			}
			if deleted == 1 {
				f.fail(t, f.guest, "GET", pathID("/articles/%d", articleID), nil, platform.CodeFail)
			} else {
				f.ok(t, f.guest, "GET", pathID("/articles/%d", articleID), nil)
			}
		}
	})
	if t.Failed() {
		return
	}
	t.Run("FR-005 controlled SMTP registration and OAuth", func(t *testing.T) {
		f.ok(t, f.guest, "GET", "/users/code?username=registered%40example.test", nil)
		code := p.code(t)
		registration := map[string]any{"username": "registered@example.test", "password": "registered-password", "code": "wrong"}
		f.fail(t, f.guest, "POST", "/register", registration, platform.CodeInvalid)
		registration["code"] = code
		f.ok(t, f.guest, "POST", "/register", registration)
		registered := newClient(t)
		profile := object(t, f.ok(t, registered, "POST", "/login", url.Values{"username": {"registered@example.test"}, "password": {"registered-password"}}))
		if profile["password"] != nil || len(list(t, profile["roleList"])) != 1 || list(t, profile["roleList"])[0] != "user" {
			t.Fatal("registration did not create an ordinary account")
		}
		f.fail(t, f.guest, "PUT", "/users/password", map[string]any{"username": "registered@example.test", "password": "replacement-password", "code": "bad"}, platform.CodeInvalid)
		f.fail(t, f.user, "POST", "/users/email", map[string]any{"email": "bound@example.test", "code": "bad"}, platform.CodeInvalid)
		qq, weibo := newClient(t), newClient(t)
		f.fail(t, qq, "POST", "/users/oauth/qq", map[string]any{"openId": "forged", "accessToken": "rejected"}, platform.CodeQQ)
		q := object(t, f.ok(t, qq, "POST", "/users/oauth/qq", map[string]any{"openId": "approved-open-id", "accessToken": "approved-qq"}))
		if q["loginType"] != float64(2) || q["password"] != nil {
			t.Fatal("QQ verified identity projection failed")
		}
		f.fail(t, weibo, "POST", "/users/oauth/weibo", map[string]any{"code": "rejected"}, platform.CodeWeibo)
		w := object(t, f.ok(t, weibo, "POST", "/users/oauth/weibo", map[string]any{"code": "approved-code"}))
		if w["loginType"] != float64(3) || w["password"] != nil {
			t.Fatal("Weibo verified identity projection failed")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("FR-006 management relationships and live authorization", func(t *testing.T) {
		f.ok(t, f.admin, "POST", "/admin/menus", map[string]any{"name": "Integration menu", "path": "/integration", "component": "Layout", "icon": "test", "orderNum": 20})
		menuID := rowID(t, f.app.DB, "SELECT id FROM tb_menu WHERE name='Integration menu'")
		f.ok(t, f.admin, "POST", "/admin/menus", map[string]any{"id": menuID, "name": "Integration edited", "path": "/integration", "component": "Layout", "icon": "test", "orderNum": 21})
		if len(list(t, f.ok(t, f.admin, "GET", "/admin/menus?keywords=Integration", nil))) != 1 || len(list(t, f.ok(t, f.admin, "GET", "/admin/role/menus", nil))) == 0 {
			t.Fatal("menu edit/options missing")
		}
		f.ok(t, f.admin, "POST", "/admin/resources", map[string]any{"resourceName": "Integration resource", "url": "/admin/roles", "requestMethod": "GET"})
		resourceID := rowID(t, f.app.DB, "SELECT id FROM tb_resource WHERE resource_name='Integration resource'")
		f.ok(t, f.admin, "POST", "/admin/resources", map[string]any{"id": resourceID, "resourceName": "Integration resource edited", "url": "/admin/roles", "requestMethod": "GET"})
		if len(list(t, f.ok(t, f.admin, "GET", "/admin/resources?keywords=Integration", nil))) != 1 || len(list(t, f.ok(t, f.admin, "GET", "/admin/role/resources", nil))) == 0 {
			t.Fatal("resource edit/options missing")
		}
		userMenus := rowID(t, f.app.DB, "SELECT id FROM tb_resource WHERE url='/admin/user/menus' AND request_method='GET'")
		role := map[string]any{"roleName": "Integration role", "roleLabel": "integration-role", "menuIdList": []int64{menuID}, "resourceIdList": []int64{resourceID, userMenus}}
		f.ok(t, f.admin, "POST", "/admin/role", role)
		roleID := rowID(t, f.app.DB, "SELECT id FROM tb_role WHERE role_label='integration-role'")
		role["id"] = roleID
		role["roleName"] = "Role edited"
		f.ok(t, f.admin, "POST", "/admin/role", role)
		if page(t, f.ok(t, f.admin, "GET", "/admin/roles?keywords=Role", nil))["count"] != float64(1) || len(list(t, f.ok(t, f.admin, "GET", "/admin/users/role", nil))) < 4 {
			t.Fatal("role edit/list missing")
		}
		f.ok(t, f.admin, "PUT", "/admin/users/role", map[string]any{"userInfoId": 11, "nickname": "Restricted reader", "roleIdList": []int64{roleID}})
		if len(list(t, f.ok(t, f.user, "GET", "/admin/user/menus", nil))) != 1 {
			t.Fatal("restricted identity received unexpected menus")
		}
		f.ok(t, f.user, "GET", "/admin/roles", nil)
		f.fail(t, f.admin, "DELETE", pathID("/admin/menus/%d", menuID), nil, platform.CodeFail)
		f.fail(t, f.admin, "DELETE", pathID("/admin/resources/%d", resourceID), nil, platform.CodeFail)
		f.fail(t, f.admin, "DELETE", "/admin/roles", []int64{roleID}, platform.CodeFail)
		f.ok(t, f.admin, "PUT", "/admin/users/role", map[string]any{"userInfoId": 11, "nickname": "Reader", "roleIdList": []int64{2}})
		f.fail(t, f.user, "GET", "/admin/roles", nil, platform.CodeFail)
		f.ok(t, f.admin, "DELETE", "/admin/roles", []int64{roleID})
		f.ok(t, f.admin, "DELETE", pathID("/admin/menus/%d", menuID), nil)
		f.ok(t, f.admin, "DELETE", pathID("/admin/resources/%d", resourceID), nil)
		if page(t, f.ok(t, f.admin, "GET", "/admin/users?keywords=Reader", nil))["count"] != float64(1) {
			t.Fatal("user filter failed")
		}
		online := page(t, f.ok(t, f.admin, "GET", "/admin/users/online", nil))
		if online["count"].(float64) < 2 {
			t.Fatal("active users missing")
		}
		f.ok(t, f.admin, "DELETE", "/admin/users/11/online", nil)
		f.fail(t, f.user, "PUT", "/users/info", map[string]any{"nickname": "Denied"}, platform.CodeFail)
		f.ok(t, f.user, "POST", "/login", url.Values{"username": {"reader@example.test"}, "password": {"fixture-password"}})
		f.ok(t, f.admin, "PUT", "/admin/users/disable", map[string]any{"id": 11, "isDisable": 1})
		f.fail(t, f.user, "POST", "/login", url.Values{"username": {"reader@example.test"}, "password": {"fixture-password"}}, platform.CodeFail)
		f.ok(t, f.admin, "PUT", "/admin/users/disable", map[string]any{"id": 11, "isDisable": 0})
		f.ok(t, f.user, "POST", "/login", url.Values{"username": {"reader@example.test"}, "password": {"fixture-password"}})
	})
	if t.Failed() {
		return
	}
	t.Run("FR-007 comments replies review notifications and messages", func(t *testing.T) {
		query := pathID("/comments?type=1&topicId=%d", articleID)
		comment := map[string]any{"type": 1, "topicId": articleID, "commentContent": "<p>Pending comment</p>", "userId": 999}
		pending := object(t, f.ok(t, f.user, "POST", "/comments", comment))
		rootID := int64(pending["commentId"].(float64))
		if page(t, f.ok(t, f.guest, "GET", query, nil))["count"] != float64(0) {
			t.Fatal("pending comment leaked")
		}
		f.ok(t, f.admin, "PUT", "/admin/comments/review", map[string]any{"idList": []int64{rootID}, "isReview": 1})
		if page(t, f.ok(t, f.guest, "GET", query, nil))["count"] != float64(1) {
			t.Fatal("approved comment missing")
		}
		if rowID(t, f.app.DB, "SELECT user_id FROM tb_comment WHERE id=?", rootID) != 11 {
			t.Fatal("comment trusted forged author")
		}
		f.ok(t, f.admin, "PUT", "/admin/website/config", map[string]any{"isCommentReview": 0, "isEmailNotice": 1})
		comment["parentId"] = rootID
		comment["replyUserId"] = 11
		comment["commentContent"] = "Reply"
		reply := object(t, f.ok(t, f.admin, "POST", "/comments", comment))
		if object(t, reply["notification"])["status"] != "accepted" {
			t.Fatal("controlled comment mail not accepted")
		}
		replies := list(t, f.ok(t, f.guest, "GET", pathID("/comments/%d/replies", rootID), nil))
		if len(replies) != 1 {
			t.Fatal("reply list/count mismatch")
		}
		for _, want := range []int64{1, 0} {
			f.ok(t, f.user, "POST", pathID("/comments/%d/like", rootID), nil)
			if rowID(t, f.app.DB, "SELECT count(*) FROM tb_like WHERE target_type=2 AND target_id=?", rootID) != want {
				t.Fatal("comment like cancellation failed")
			}
		}
		p.reject.Store(true)
		comment["commentContent"] = "Reply with failed notification"
		failed := object(t, f.ok(t, f.admin, "POST", "/comments", comment))
		if object(t, failed["notification"])["status"] != "failed" {
			t.Fatal("mail rejection hidden")
		}
		p.reject.Store(false)
		if page(t, f.ok(t, f.admin, "GET", "/admin/comments", nil))["count"].(float64) < 3 {
			t.Fatal("admin comments missing saved replies")
		}
		f.ok(t, f.admin, "DELETE", "/admin/comments", []int64{rootID})
		if page(t, f.ok(t, f.guest, "GET", query, nil))["count"] != float64(0) {
			t.Fatal("comment deletion did not cascade replies")
		}
		message := map[string]any{"nickname": "Visitor", "avatar": images["/admin/config/images"], "messageContent": "Pending message", "time": 10}
		started := time.Now()
		f.ok(t, f.guest, "POST", "/messages", message)
		messageID := rowID(t, f.app.DB, "SELECT id FROM tb_message WHERE message_content='Pending message'")
		if len(list(t, f.ok(t, f.guest, "GET", "/messages", nil))) != 0 {
			t.Fatal("pending message leaked")
		}
		f.ok(t, f.admin, "PUT", "/admin/messages/review", map[string]any{"idList": []int64{messageID}, "isReview": 1})
		if len(list(t, f.ok(t, f.guest, "GET", "/messages", nil))) != 1 {
			t.Fatal("reviewed message missing")
		}
		f.ok(t, f.admin, "PUT", "/admin/website/config", map[string]any{"isMessageReview": 0, "isEmailNotice": 0})
		time.Sleep(time.Until(started.Add(5*time.Second + 20*time.Millisecond)))
		message["messageContent"] = "Public without review"
		f.ok(t, f.guest, "POST", "/messages", message)
		if len(list(t, f.ok(t, f.guest, "GET", "/messages", nil))) != 2 {
			t.Fatal("disabled message review did not publish immediately")
		}
		if page(t, f.ok(t, f.admin, "GET", "/admin/messages", nil))["count"] != float64(2) {
			t.Fatal("admin message count mismatch")
		}
		other := rowID(t, f.app.DB, "SELECT id FROM tb_message WHERE message_content='Public without review'")
		f.ok(t, f.admin, "DELETE", "/admin/messages", []int64{messageID, other})
		if len(list(t, f.ok(t, f.guest, "GET", "/messages", nil))) != 0 {
			t.Fatal("deleted messages still public")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("FR-008 talks albums photos and recovery", func(t *testing.T) {
		talk := map[string]any{"content": "Integration talk", "images": "[\"" + images["/admin/talks/images"] + "\"]", "isTop": 1, "status": 1}
		f.ok(t, f.admin, "POST", "/admin/talks", talk)
		talkID := rowID(t, f.app.DB, "SELECT id FROM tb_talk WHERE content='Integration talk'")
		talk["id"] = talkID
		if len(list(t, f.ok(t, f.guest, "GET", "/home/talks", nil))) == 0 || page(t, f.ok(t, f.guest, "GET", "/talks", nil))["count"].(float64) < 2 {
			t.Fatal("talk lists missing public data")
		}
		detail := object(t, f.ok(t, f.guest, "GET", pathID("/talks/%d", talkID), nil))
		if len(list(t, detail["imgList"])) != 1 {
			t.Fatal("talk image projection missing")
		}
		for _, want := range []float64{1, 0} {
			f.ok(t, f.user, "POST", pathID("/talks/%d/like", talkID), nil)
			if object(t, f.ok(t, f.guest, "GET", pathID("/talks/%d", talkID), nil))["likeCount"] != want {
				t.Fatal("talk like cancellation failed")
			}
		}
		talk["status"] = 2
		f.ok(t, f.admin, "POST", "/admin/talks", talk)
		f.fail(t, f.guest, "GET", pathID("/talks/%d", talkID), nil, platform.CodeFail)
		if object(t, f.ok(t, f.admin, "GET", pathID("/admin/talks/%d", talkID), nil))["status"] != float64(2) {
			t.Fatal("talk visibility edit missing")
		}
		if page(t, f.ok(t, f.admin, "GET", "/admin/talks?status=2", nil))["count"] != float64(1) {
			t.Fatal("admin talk filter failed")
		}
		f.ok(t, f.admin, "DELETE", "/admin/talks", []int64{talkID})
		album := map[string]any{"albumName": "Album A", "albumDesc": "Integration album", "albumCover": images["/admin/photos/albums/cover"], "status": 1}
		f.ok(t, f.admin, "POST", "/admin/photos/albums", album)
		albumID := rowID(t, f.app.DB, "SELECT id FROM tb_photo_album WHERE album_name='Album A'")
		album["id"] = albumID
		album["albumDesc"] = "Edited album"
		f.ok(t, f.admin, "POST", "/admin/photos/albums", album)
		if object(t, f.ok(t, f.admin, "GET", pathID("/admin/photos/albums/%d/info", albumID), nil))["albumDesc"] != "Edited album" {
			t.Fatal("album edit did not persist")
		}
		page(t, f.ok(t, f.admin, "GET", "/admin/photos/albums", nil))
		list(t, f.ok(t, f.admin, "GET", "/admin/photos/albums/info", nil))
		list(t, f.ok(t, f.guest, "GET", "/photos/albums", nil))
		f.ok(t, f.admin, "POST", "/admin/photos", map[string]any{"albumId": albumID, "photoUrlList": []string{images["/admin/articles/images"], images["/admin/config/images"]}})
		photos := page(t, f.ok(t, f.admin, "GET", pathID("/admin/photos?albumId=%d", albumID), nil))
		records := list(t, photos["recordList"])
		if len(records) != 2 {
			t.Fatal("photo batch not saved")
		}
		photoID := int64(object(t, records[0])["id"].(float64))
		otherID := int64(object(t, records[1])["id"].(float64))
		f.ok(t, f.admin, "PUT", "/admin/photos", map[string]any{"id": photoID, "photoName": "Edited photo", "photoDesc": "Caption"})
		f.ok(t, f.admin, "DELETE", pathID("/admin/photos/albums/%d", albumID), nil)
		if rowID(t, f.app.DB, "SELECT count(*) FROM tb_photo WHERE album_id=? AND is_delete=1", albumID) != 2 {
			t.Fatal("album deletion did not recycle photos")
		}
		f.ok(t, f.admin, "PUT", "/admin/photos/delete", map[string]any{"idList": []int64{photoID}, "isDelete": 0})
		if rowID(t, f.app.DB, "SELECT is_delete FROM tb_photo_album WHERE id=?", albumID) != 0 {
			t.Fatal("photo recovery did not restore album")
		}
		public := object(t, f.ok(t, f.guest, "GET", pathID("/albums/%d/photos", albumID), nil))
		if len(list(t, public["photoList"])) != 1 {
			t.Fatal("restored public album contents wrong")
		}
		album["id"] = nil
		album["albumName"] = "Album B"
		f.ok(t, f.admin, "POST", "/admin/photos/albums", album)
		destination := rowID(t, f.app.DB, "SELECT id FROM tb_photo_album WHERE album_name='Album B'")
		f.ok(t, f.admin, "PUT", "/admin/photos/album", map[string]any{"albumId": destination, "photoIdList": []int64{photoID}})
		if rowID(t, f.app.DB, "SELECT album_id FROM tb_photo WHERE id=?", photoID) != destination {
			t.Fatal("photo move failed")
		}
		f.ok(t, f.admin, "PUT", "/admin/photos/delete", map[string]any{"idList": []int64{photoID, otherID}, "isDelete": 1})
		f.ok(t, f.admin, "DELETE", "/admin/photos", []int64{photoID, otherID})
		if rowID(t, f.app.DB, "SELECT count(*) FROM tb_photo WHERE id IN (?,?)", photoID, otherID) != 0 {
			t.Fatal("permanent photo deletion failed")
		}
		album["albumName"] = "Empty album"
		f.ok(t, f.admin, "POST", "/admin/photos/albums", album)
		empty := rowID(t, f.app.DB, "SELECT id FROM tb_photo_album WHERE album_name='Empty album'")
		f.ok(t, f.admin, "DELETE", pathID("/admin/photos/albums/%d", empty), nil)
		if rowID(t, f.app.DB, "SELECT count(*) FROM tb_photo_album WHERE id=?", empty) != 0 {
			t.Fatal("empty album was not deleted")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("FR-010 FR-011 FR-012 visit facts logs export import and voice", func(t *testing.T) {
		before := rowID(t, f.app.DB, "SELECT count(*) FROM tb_visit_record")
		f.ok(t, f.guest, "POST", "/report", nil)
		f.ok(t, f.guest, "POST", "/report", nil)
		if rowID(t, f.app.DB, "SELECT count(*) FROM tb_visit_record") != before+1 {
			t.Fatal("same IP/browser/OS day was not deduplicated as one visit fact")
		}
		object(t, f.ok(t, f.admin, "GET", "/admin", nil))
		list(t, f.ok(t, f.admin, "GET", "/admin/users/area?type=1", nil))
		logs := page(t, f.ok(t, f.admin, "GET", "/admin/operation/logs", nil))
		if logs["count"].(float64) == 0 {
			t.Fatal("management operations were not audited")
		}
		logID := int64(object(t, list(t, logs["recordList"])[0])["id"].(float64))
		f.ok(t, f.admin, "DELETE", "/admin/operation/logs", []int64{logID})
		if rowID(t, f.app.DB, "SELECT count(*) FROM tb_operation_log WHERE id=?", logID) != 0 || rowID(t, f.app.DB, "SELECT count(*) FROM tb_article WHERE id=?", articleID) != 1 {
			t.Fatal("log deletion changed business content")
		}
		exports := list(t, f.ok(t, f.admin, "POST", "/admin/articles/export", []int64{articleID}))
		location := exports[0].(string)
		digest := sha256.Sum256([]byte(content))
		if !strings.Contains(location, fmt.Sprintf("/exports/%d-%s.md", articleID, hex.EncodeToString(digest[:]))) {
			t.Fatal("export key lacks article identity and body hash")
		}
		r, err := f.admin.Get(location)
		must(t, err)
		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		must(t, err)
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Disposition"))
		must(t, err)
		if !bytes.Equal(body, []byte(content)) || params["filename"] != "Integration article.md" {
			t.Fatal("export bytes or original filename changed")
		}
		imported := f.upload(t, f.admin, "/admin/articles/import", "Imported integration.md", "text/markdown", []byte("# Imported body"), nil)
		if !imported.result.Flag {
			t.Fatalf("import failed: %#v", imported.result)
		}
		id := rowID(t, f.app.DB, "SELECT id FROM tb_article WHERE article_title='Imported integration'")
		if object(t, f.ok(t, f.admin, "GET", pathID("/admin/articles/%d", id), nil))["articleContent"] != "# Imported body" {
			t.Fatal("import body differs")
		}
		countBefore := rowID(t, f.app.DB, "SELECT count(*) FROM tb_article")
		bad := f.upload(t, f.admin, "/admin/articles/import", "bad.exe", "application/octet-stream", []byte("bad"), nil)
		if bad.result.Flag || rowID(t, f.app.DB, "SELECT count(*) FROM tb_article") != countBefore {
			t.Fatal("invalid import left partial articles")
		}
		voice := f.upload(t, f.guest, "/voice", "voice.wav", "audio/wav", wavData(), map[string]string{"type": "5", "nickname": "Visitor", "avatar": images["/admin/config/images"]})
		if !voice.result.Flag || voice.result.Data != nil {
			t.Fatal("voice HTTP result incompatible")
		}
		voiceURL := ""
		must(t, f.app.DB.QueryRow("SELECT content FROM tb_chat_record WHERE type=5 ORDER BY id DESC LIMIT 1").Scan(&voiceURL))
		r, err = f.guest.Get(voiceURL)
		must(t, err)
		body, err = io.ReadAll(r.Body)
		r.Body.Close()
		must(t, err)
		if !bytes.Equal(body, wavData()) {
			t.Fatal("saved voice unreadable")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("FR-002 FR-003 cleanup and FR-004 password/logout", func(t *testing.T) {
		f.ok(t, f.admin, "DELETE", "/admin/articles", []int64{articleID})
		f.fail(t, f.guest, "GET", pathID("/articles/%d", articleID), nil, platform.CodeFail)
		if rowID(t, f.app.DB, "SELECT count(*) FROM tb_article_tag WHERE article_id=?", articleID) != 0 {
			t.Fatal("permanent article deletion left links")
		}
		f.ok(t, f.admin, "DELETE", "/admin/categories", []int64{category})
		f.ok(t, f.admin, "DELETE", "/admin/tags", []int64{tag})
		f.ok(t, f.admin, "DELETE", "/admin/links", []int64{link})
		if rowID(t, f.app.DB, "SELECT count(*) FROM tb_friend_link WHERE id=?", link) != 0 {
			t.Fatal("link deletion missing")
		}
		f.ok(t, f.admin, "PUT", "/admin/users/password", map[string]any{"oldPassword": f.adminPassword, "newPassword": "integration-new-password"})
		f.fail(t, f.admin, "GET", "/admin/user/menus", nil, platform.CodeFail)
		f.ok(t, f.admin, "POST", "/login", url.Values{"username": {f.adminUsername}, "password": {"integration-new-password"}})
		f.ok(t, f.user, "POST", "/logout", nil)
		f.fail(t, f.user, "PUT", "/users/info", map[string]any{"nickname": "must fail"}, platform.CodeFail)
	})
	if len(specs()) != 108 {
		t.Fatalf("business route contract changed: %d", len(specs()))
	}
	for _, spec := range specs() {
		if f.hits[spec.Method+" "+spec.Path] == 0 {
			t.Errorf("no behavior exercised for %s %s", spec.Method, spec.Path)
		}
	}
}

func assertImage(t *testing.T, client *http.Client, location, wantMIME string) {
	t.Helper()
	r, err := client.Get(location)
	must(t, err)
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	must(t, err)
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != wantMIME {
		t.Fatalf("image HTTP type=%s status=%d", r.Header.Get("Content-Type"), r.StatusCode)
	}
	image, _, err := image.Decode(bytes.NewReader(data))
	must(t, err)
	if image.Bounds().Dx() != 2 || image.Bounds().Dy() != 2 {
		t.Fatal("uploaded image did not decode")
	}
}

func TestControlledOSSUploadURLReadsActualBytes(t *testing.T) {
	p := newProviders(t)
	f := newFixture(t, func(cfg *platform.Config) {
		p.configure(cfg)
		for key, value := range map[string]string{"BLOG_UPLOAD_MODE": "oss", "BLOG_OSS_ENDPOINT": "https://oss.fixture.test", "BLOG_OSS_REGION": "cn-hangzhou", "BLOG_OSS_BUCKET": "integration-bucket", "BLOG_OSS_PUBLIC_URL": "https://cdn.fixture.test", "BLOG_OSS_ACCESS_KEY_ID": "controlled-id", "BLOG_OSS_ACCESS_KEY_SECRET": "controlled-secret"} {
			cfg.Values[key] = value
		}
	})
	r := f.upload(t, f.admin, "/admin/articles/images", "oss.png", "image/png", imageData(t, false), nil)
	if !r.result.Flag {
		t.Fatalf("controlled OSS upload failed: %#v", r.result)
	}
	location := r.result.Data.(string)
	if !strings.HasPrefix(location, "https://cdn.fixture.test/articles/") {
		t.Fatal("OSS URL projection failed")
	}
	assertImage(t, f.guest, location, "image/png")
}

func TestArticlePaginationObjectsRemainSafeForOriginalVue(t *testing.T) {
	f := newFixture(t)
	check := func(id int64, previous, next any) {
		t.Helper()
		detail := object(t, f.ok(t, f.guest, "GET", pathID("/articles/%d", id), nil))
		for key, want := range map[string]any{"lastArticle": previous, "nextArticle": next} {
			neighbor := object(t, detail[key])
			if neighbor["id"] != want {
				t.Fatalf("%s=%v want %v", key, neighbor["id"], want)
			}
			if len(neighbor) != 3 {
				t.Fatal("neighbor must retain id/title/cover fields")
			}
		}
	}
	check(1, nil, nil)
	f.ok(t, f.admin, "POST", "/admin/articles", map[string]any{"articleTitle": "Boundary article", "articleContent": "# Boundary body", "categoryName": "生活记录", "tagNameList": []string{"开始"}, "status": 1, "type": 1})
	id := rowID(t, f.app.DB, "SELECT id FROM tb_article WHERE article_title='Boundary article'")
	check(1, nil, float64(id))
	check(id, float64(1), nil)
	execSQL(t, f.app.DB, "UPDATE tb_article SET status=2 WHERE id=1")
	check(id, nil, nil)
}

func TestInitialAdminWorksWithUnchangedCLoginRule(t *testing.T) {
	f := newFixture(t)
	// The original LoginModel.vue rejects usernames outside this exact shape.
	if !regexp.MustCompile(`^[A-Za-z0-9一-龥]+@[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)+$`).MatchString(f.adminUsername) {
		t.Fatal("generated administrator cannot pass the C frontend email validation")
	}
	profile := object(t, f.ok(t, newClient(t), "POST", "/login", url.Values{"username": {f.adminUsername}, "password": {f.adminPassword}}))
	if profile["userInfoId"] != float64(1) {
		t.Fatal("initial account did not authenticate")
	}
}
