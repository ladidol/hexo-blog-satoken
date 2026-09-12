package site

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"blog-go/internal/platform"

	"github.com/mattn/go-sqlite3"
)

// 用途：拥有站点配置、关于与页面的状态、数据访问及下列显式依赖。
type Service struct {
	db  *sql.DB
	cfg platform.Config
	now func() time.Time
}

// 用途：构造站点配置、关于与页面，由 app 唯一装配依赖。
func New(db *sql.DB, cfg platform.Config, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.TimeZone == nil {
		cfg.TimeZone = time.UTC
	}
	return &Service{db: db, cfg: cfg, now: now}
}

// 用途：提供当前持久网站配置给文章默认封面、账号头像和审核业务。
func (s *Service) Config(ctx context.Context) (platform.Object, error) {
	return readConfig(ctx, s.db)
}

// 用途：承接原 GET / 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) GetBlogHomeInfo(ctx context.Context, in platform.Input) (any, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	website, err := readConfig(ctx, tx)
	if err != nil {
		return nil, err
	}
	var articleCount, categoryCount, tagCount, visits int64
	err = tx.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM tb_article WHERE status=1 AND is_delete=0),
		(SELECT COUNT(*) FROM tb_category),
		(SELECT COUNT(*) FROM tb_tag),
		(SELECT COUNT(*) FROM tb_visit_record)`).Scan(&articleCount, &categoryCount, &tagCount, &visits)
	if err != nil {
		return nil, fmt.Errorf("read home statistics: %w", err)
	}
	pages, err := readPages(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return platform.Object{
		"articleCount": articleCount, "categoryCount": categoryCount, "tagCount": tagCount,
		"viewsCount": strconv.FormatInt(visits, 10), "websiteConfig": website, "pageList": pages,
	}, nil
}

// 用途：承接原 PUT /admin/website/config 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateWebsiteConfig(ctx context.Context, in platform.Input) (any, error) {
	changes, err := decodeConfig(in.Body, false)
	if err != nil {
		return nil, err
	}
	// A single transaction prevents partial edits from overwriting another edit.
	// Omitted business fields retain their saved values; runtime keys are rejected.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	website, err := readConfig(ctx, tx)
	if err != nil {
		return nil, err
	}
	for key, value := range changes {
		website[key] = value
	}
	raw, err := json.Marshal(website)
	if err != nil {
		return nil, fmt.Errorf("encode website configuration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tb_website_config SET config=?,update_time=? WHERE id=1", string(raw), s.stamp()); err != nil {
		return nil, fmt.Errorf("save website configuration: %w", err)
	}
	return nil, tx.Commit()
}

// 用途：承接原 GET /admin/website/config 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) GetWebsiteConfig(ctx context.Context, in platform.Input) (any, error) {
	return s.Config(ctx)
}

// 用途：承接原 GET /about 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) GetAbout(ctx context.Context, in platform.Input) (any, error) {
	var content string
	err := s.db.QueryRowContext(ctx, "SELECT content FROM tb_about WHERE id=1").Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return nil, fmt.Errorf("read about content: %w", err)
	}
	return content, nil
}

// 用途：承接原 PUT /admin/about 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateAbout(ctx context.Context, in platform.Input) (any, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(in.Body, &fields); err != nil || fields == nil {
		return nil, invalid("关于正文参数不正确")
	}
	var content string
	if raw, ok := fields["aboutContent"]; !ok || string(raw) == "null" || json.Unmarshal(raw, &content) != nil {
		return nil, invalid("关于正文必须是字符串")
	}
	stamp := s.stamp()
	_, err := s.db.ExecContext(ctx, `INSERT INTO tb_about(id,content,create_time) VALUES(1,?,?)
		ON CONFLICT(id) DO UPDATE SET content=excluded.content,update_time=?`, content, stamp, stamp)
	if err != nil {
		return nil, fmt.Errorf("save about content: %w", err)
	}
	return nil, nil
}

// 用途：承接原 DELETE /admin/pages/{pageId} 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeletePage(ctx context.Context, in platform.Input) (any, error) {
	id, err := strconv.ParseInt(in.Params["pageId"], 10, 32)
	if err != nil || id <= 0 {
		return nil, invalid("页面编号不正确")
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM tb_page WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("delete page: %w", err)
	}
	return nil, nil
}

// 用途：承接原 POST /admin/pages 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveOrUpdatePage(ctx context.Context, in platform.Input) (any, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(in.Body, &fields); err != nil || fields == nil {
		return nil, invalid("页面参数不正确")
	}
	values := make(map[string]string, 3)
	for _, key := range []string{"pageName", "pageLabel", "pageCover"} {
		var value string
		if raw, ok := fields[key]; !ok || json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" {
			return nil, invalid("页面名称、标签和封面不能为空")
		}
		values[key] = value
	}
	if !validURL(values["pageCover"], false) {
		return nil, invalid("页面封面地址不正确")
	}
	var id any
	if raw, ok := fields["id"]; ok && string(raw) != "null" {
		var number int32
		if json.Unmarshal(raw, &number) != nil || number <= 0 {
			return nil, invalid("页面编号不正确")
		}
		id = number
	}
	stamp := s.stamp()
	_, err := s.db.ExecContext(ctx, `INSERT INTO tb_page(id,page_name,page_label,page_cover,create_time) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET page_name=excluded.page_name,page_label=excluded.page_label,
		page_cover=excluded.page_cover,update_time=?`, id, values["pageName"], values["pageLabel"], values["pageCover"], stamp, stamp)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			return nil, errors.Join(&platform.Error{Code: platform.CodeFail, Message: "页面标签已存在"}, err)
		}
		return nil, fmt.Errorf("save page: %w", err)
	}
	return nil, nil
}

// 用途：承接原 GET /admin/pages 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListPages(ctx context.Context, in platform.Input) (any, error) {
	return readPages(ctx, s.db)
}

func (s *Service) stamp() string {
	return s.now().In(s.cfg.TimeZone).Format("2006-01-02T15:04:05")
}

// Both standalone reads and the home snapshot project the same business fields.
type siteReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readConfig(ctx context.Context, reader siteReader) (platform.Object, error) {
	var raw string
	if err := reader.QueryRowContext(ctx, "SELECT config FROM tb_website_config WHERE id=1").Scan(&raw); err != nil {
		return nil, fmt.Errorf("read website configuration: %w", err)
	}
	website, err := decodeConfig([]byte(raw), true)
	if err != nil {
		return nil, fmt.Errorf("stored website configuration is invalid: %w", err)
	}
	return website, nil
}

func readPages(ctx context.Context, reader siteReader) ([]platform.Object, error) {
	rows, err := reader.QueryContext(ctx, "SELECT id,page_name,page_label,page_cover FROM tb_page ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("read pages: %w", err)
	}
	defer rows.Close()
	pages := make([]platform.Object, 0)
	for rows.Next() {
		var id int64
		var name, cover string
		var label sql.NullString
		if err := rows.Scan(&id, &name, &label, &cover); err != nil {
			return nil, fmt.Errorf("read page: %w", err)
		}
		var pageLabel any
		if label.Valid {
			pageLabel = label.String
		}
		pages = append(pages, platform.Object{"id": id, "pageName": name, "pageLabel": pageLabel, "pageCover": cover})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pages: %w", err)
	}
	return pages, nil
}

// The keys are the frozen WebsiteConfigVO projection. qq is an account number;
// socialUrlList holds display-entry names, not URLs (as consumed by both UIs).
var configFields = map[string]string{
	"websiteAvatar": "http", "websiteName": "string", "websiteAuthor": "string",
	"websiteIntro": "string", "websiteNotice": "string", "websiteCreateTime": "string",
	"websiteRecordNo": "string", "socialLoginList": "list", "socialUrlList": "list",
	"qq": "string", "github": "http", "gitee": "http", "touristAvatar": "http", "userAvatar": "http",
	"isCommentReview": "flag", "isMessageReview": "flag", "isEmailNotice": "flag", "isReward": "flag",
	"weiXinQRCode": "http", "alipayQRCode": "http", "articleCover": "http",
	"isChatRoom": "flag", "websocketUrl": "ws", "isMusicPlayer": "flag",
}

func decodeConfig(raw []byte, complete bool) (platform.Object, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) == 0 {
		return nil, invalid("网站配置参数不正确")
	}
	if complete && len(fields) != len(configFields) {
		return nil, invalid("网站配置字段不完整")
	}
	website := make(platform.Object, len(fields))
	for key, rawValue := range fields {
		kind, known := configFields[key]
		if !known {
			return nil, invalid("网站配置包含未知字段")
		}
		switch kind {
		case "flag":
			var flag int
			if string(rawValue) == "null" || json.Unmarshal(rawValue, &flag) != nil || (flag != 0 && flag != 1) {
				return nil, invalid("配置开关只接受 0 或 1")
			}
			website[key] = float64(flag)
		case "list":
			var members []any
			if json.Unmarshal(rawValue, &members) != nil {
				return nil, invalid("社交入口列表必须是字符串数组")
			}
			list := make([]string, 0, len(members))
			for _, member := range members {
				value, ok := member.(string)
				if !ok {
					return nil, invalid("社交入口列表必须是字符串数组")
				}
				list = append(list, value)
			}
			website[key] = list
		default:
			var value string
			if string(rawValue) == "null" || json.Unmarshal(rawValue, &value) != nil {
				return nil, invalid("网站配置字段必须是字符串")
			}
			if (kind == "http" || kind == "ws") && value != "" && !validURL(value, kind == "ws") {
				return nil, invalid("网站配置地址不正确")
			}
			website[key] = value
		}
	}
	return website, nil
}

func validURL(value string, websocket bool) bool {
	if strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return false
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return false
		}
	}
	if websocket {
		return u.Scheme == "ws" || u.Scheme == "wss"
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func invalid(message string) error {
	return &platform.Error{Code: platform.CodeInvalid, Message: message}
}

// 用途：将本模块 schema.sql 交给唯一迁移器，业务逻辑不自动建表。
//
//go:embed schema.sql
var Schema string

// Public protocol projections use platform.Object; no database row is serialized directly.
// Integer/Long: JSON number; Date: Unix milliseconds; LocalDateTime: yyyy-MM-ddTHH:mm:ss.
// Optional fields retain null; lists/sets are arrays. UserDetailDTO never includes password.
// BlogHomeInfoDTO: articleCount:Integer, categoryCount:Integer, tagCount:Integer, viewsCount:String, websiteConfig:WebsiteConfigVO, pageList:PageVO[]
// WebsiteConfigVO: websiteAvatar:String, websiteName:String, websiteAuthor:String, websiteIntro:String, websiteNotice:String, websiteCreateTime:String, websiteRecordNo:String, socialLoginList:String[], socialUrlList:String[], qq:String, github:String, gitee:String, touristAvatar:String, userAvatar:String, isCommentReview:Integer, isMessageReview:Integer, isEmailNotice:Integer, isReward:Integer, weiXinQRCode:String, alipayQRCode:String, articleCover:String, isChatRoom:Integer, websocketUrl:String, isMusicPlayer:Integer
// BlogInfoVO: aboutContent:String
// PageVO: id:Integer, pageName:String, pageLabel:String, pageCover:String
