package bootstrap

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"blog-go/internal/analytics"
	"blog-go/internal/article"
	"blog-go/internal/catalog"
	"blog-go/internal/community"
	"blog-go/internal/files"
	"blog-go/internal/identity"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
)

// 用途：向首次空库写入可运行种子与可信默认文件，不覆盖已编辑数据。
func Seed(ctx context.Context, tx *sql.Tx, cfg platform.Config) (err error) {
	const initialUsername = "localadmin@example.test"
	if tx == nil || cfg.DataDir == "" || cfg.TimeZone == nil || cfg.PublicURL == "" {
		return errors.New("seed requires a transaction and local configuration")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	credentialPath := filepath.Join(cfg.DataDir, "initial-admin.txt")
	assetDir := filepath.Join(cfg.DataDir, "files", "defaults")
	destinations := []string{credentialPath, filepath.Join(assetDir, "avatar.svg"), filepath.Join(assetDir, "cover.svg")}
	for _, destination := range destinations {
		if _, statErr := os.Lstat(destination); statErr == nil {
			return errors.New("initial seed file already exists; existing files were preserved")
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("inspect seed file: %w", statErr)
		}
	}
	var created []string
	defer func() {
		if err != nil {
			for _, file := range created {
				if removeErr := os.Remove(file); removeErr != nil {
					err = errors.Join(err, fmt.Errorf("remove incomplete seed file: %w", removeErr))
				}
			}
		}
	}()
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return fmt.Errorf("generate local administrator credential: %w", err)
	}
	password := base64.RawURLEncoding.EncodeToString(random)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return fmt.Errorf("hash local administrator credential: %w", err)
	}
	now := time.Now().In(cfg.TimeZone)
	stamp := now.Format("2006-01-02T15:04:05")
	publicURL := strings.TrimRight(cfg.PublicURL, "/")
	avatarURL := publicURL + "/files/defaults/avatar.svg"
	coverURL := publicURL + "/files/defaults/cover.svg"
	websocketURL := strings.Replace(strings.Replace(publicURL, "https://", "wss://", 1), "http://", "ws://", 1) + "/websocket"
	website := platform.Object{
		"websiteAvatar": avatarURL, "websiteName": "本地博客", "websiteAuthor": "本地开发者",
		"websiteIntro": "", "websiteNotice": "", "websiteCreateTime": now.Format("2006-01-02"),
		"websiteRecordNo": "", "socialLoginList": []string{"qq", "weibo"}, "socialUrlList": []string{},
		"qq": "", "github": "", "gitee": "", "touristAvatar": avatarURL, "userAvatar": avatarURL,
		"isCommentReview": 1, "isMessageReview": 1, "isEmailNotice": 0, "isReward": 0,
		"weiXinQRCode": "", "alipayQRCode": "", "articleCover": coverURL,
		"isChatRoom": 1, "websocketUrl": websocketURL, "isMusicPlayer": 0,
	}
	websiteJSON, err := json.Marshal(website)
	if err != nil {
		return fmt.Errorf("encode initial website configuration: %w", err)
	}
	exec := func(query string, args ...any) error {
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert initial database record: %w", err)
		}
		return nil
	}
	for _, role := range []struct {
		id          int
		name, label string
	}{{1, "管理员", "admin"}, {2, "用户", "user"}, {3, "测试", "test"}} {
		if err := exec("INSERT INTO tb_role(id,role_name,role_label,is_disable,create_time) VALUES(?,?,?,0,?)", role.id, role.name, role.label, stamp); err != nil {
			return err
		}
	}
	var menus []struct {
		ID                                      int64
		Name, Path, Component, Icon, CreateTime string
		UpdateTime                              *string
		OrderNum                                int
		ParentID                                *int64
		IsHidden                                int
	}
	if err := json.Unmarshal(MenuSeed, &menus); err != nil {
		return fmt.Errorf("decode menu seed: %w", err)
	}
	for _, menu := range menus {
		if err := exec("INSERT INTO tb_menu(id,name,path,component,icon,create_time,update_time,order_num,parent_id,is_hidden) VALUES(?,?,?,?,?,?,?,?,?,?)",
			menu.ID, menu.Name, menu.Path, menu.Component, menu.Icon, menu.CreateTime, menu.UpdateTime, menu.OrderNum, menu.ParentID, menu.IsHidden); err != nil {
			return err
		}
		if err := exec("INSERT INTO tb_role_menu(role_id,menu_id) VALUES(1,?)", menu.ID); err != nil {
			return err
		}
		if menu.ID == 1 || menu.ID == 5 {
			if err := exec("INSERT INTO tb_role_menu(role_id,menu_id) VALUES(3,?)", menu.ID); err != nil {
				return err
			}
		}
	}
	groups := [][]platform.RouteSpec{identity.RouteSpecs, article.RouteSpecs, catalog.RouteSpecs,
		community.RouteSpecs, media.RouteSpecs, site.RouteSpecs, analytics.RouteSpecs, files.RouteSpecs}
	for _, group := range groups {
		for _, spec := range group {
			if spec.Policy != "admin" {
				continue
			}
			inserted, err := tx.ExecContext(ctx,
				"INSERT INTO tb_resource(resource_name,url,request_method,is_anonymous,create_time) VALUES(?,?,?,0,?)",
				spec.Operation, spec.Path, spec.Method, stamp)
			if err != nil {
				return fmt.Errorf("insert management resource: %w", err)
			}
			resourceID, err := inserted.LastInsertId()
			if err != nil {
				return fmt.Errorf("read management resource ID: %w", err)
			}
			if err := exec("INSERT INTO tb_role_resource(role_id,resource_id) VALUES(1,?)", resourceID); err != nil {
				return err
			}
			if spec.Method == "GET" && (spec.Path == "/admin" || spec.Path == "/admin/user/menus") {
				if err := exec("INSERT INTO tb_role_resource(role_id,resource_id) VALUES(3,?)", resourceID); err != nil {
					return err
				}
			}
		}
	}
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO tb_user_info(id,nickname,avatar,is_disable,create_time) VALUES(1,?,?,0,?)", []any{"本地开发者", avatarURL, stamp}},
		{"INSERT INTO tb_user_auth(id,user_info_id,username,password,login_type,create_time) VALUES(1,1,?,?,1,?)", []any{initialUsername, string(hash), stamp}},
		{"INSERT INTO tb_user_role(user_id,role_id) VALUES(1,1)", nil},
		{"INSERT INTO tb_website_config(id,config,create_time) VALUES(1,?,?)", []any{string(websiteJSON), stamp}},
		{"INSERT INTO tb_about(id,content,create_time) VALUES(1,?,?)", []any{"# 关于本站\n\n这是在本机运行的 Go 博客。欢迎记录和分享。", stamp}},
		{"INSERT INTO tb_category(id,category_name,create_time) VALUES(1,?,?)", []any{"生活记录", stamp}},
		{"INSERT INTO tb_tag(id,tag_name,create_time) VALUES(1,?,?)", []any{"开始", stamp}},
		{"INSERT INTO tb_article(id,user_id,category_id,article_cover,article_title,article_content,type,is_top,is_delete,status,create_time) VALUES(1,1,1,?,?,?,1,0,0,1,?)",
			[]any{coverURL, "欢迎来到本地博客", "# 你好，博客\n\n这篇文章保存在本机 SQLite 中，可以在管理端编辑。", stamp}},
		{"INSERT INTO tb_article_tag(article_id,tag_id) VALUES(1,1)", nil},
		{"INSERT INTO tb_friend_link(link_name,link_avatar,link_address,link_intro,create_time) VALUES(?,?,?,?,?)",
			[]any{"本地博客", avatarURL, cfg.FrontendURL, "记录自己的想法", stamp}},
		{"INSERT INTO tb_talk(id,user_id,content,images,is_top,status,create_time) VALUES(1,1,?,'[]',0,1,?)", []any{"博客已经准备好，写下今天的第一条记录。", stamp}},
		{"INSERT INTO tb_photo_album(id,album_name,album_desc,album_cover,is_delete,status,create_time) VALUES(1,?,?,?,0,1,?)",
			[]any{"本地相册", "从这里开始收集生活片段", coverURL, stamp}},
		{"INSERT INTO tb_photo(album_id,photo_name,photo_desc,photo_src,is_delete,create_time) VALUES(1,?,?,?,0,?)",
			[]any{"山景", "内置默认封面", coverURL, stamp}},
	}
	for _, statement := range statements {
		if err := exec(statement.query, statement.args...); err != nil {
			return err
		}
	}
	for _, page := range [][2]string{{"首页", "home"}, {"文章", "articleList"}, {"分类", "category"}, {"标签", "tag"},
		{"归档", "archive"}, {"说说", "talk"}, {"相册", "album"}, {"关于", "about"}, {"友链", "link"}, {"留言", "message"}, {"个人中心", "user"}} {
		if err := exec("INSERT INTO tb_page(page_name,page_label,page_cover,create_time) VALUES(?,?,?,?)", page[0], page[1], coverURL, stamp); err != nil {
			return err
		}
	}
	for _, directory := range []string{filepath.Join(cfg.DataDir, "files"), assetDir} {
		if err := os.Mkdir(directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create seed asset directory: %w", err)
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("seed asset directory must be a local directory")
		}
	}
	for _, file := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{filepath.Join(assetDir, "avatar.svg"), AvatarSVG, 0644},
		{filepath.Join(assetDir, "cover.svg"), CoverSVG, 0644},
		{credentialPath, []byte("username=" + initialUsername + "\npassword=" + password + "\n"), 0600},
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeNewFile(file.path, file.data, file.mode); err != nil {
			return fmt.Errorf("write initial seed file: %w", err)
		}
		created = append(created, file.path)
	}
	if rollback, ok := ctx.Value((*platform.MigrationFiles)(nil)).(*platform.MigrationFiles); ok {
		rollback.Paths = append(rollback.Paths, created...)
	}
	return nil
}

func writeNewFile(path string, data []byte, mode os.FileMode) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
		if err != nil {
			err = errors.Join(err, os.Remove(path))
		}
	}()
	if _, err = file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

// MenuSeed preserves the original 33 menu declarations; Seed owns insertion.
//
//go:embed seed-menus.json
var MenuSeed []byte

//go:embed assets/avatar.svg
var AvatarSVG []byte

//go:embed assets/cover.svg
var CoverSVG []byte
