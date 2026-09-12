package catalog

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"blog-go/internal/platform"
)

// 用途：拥有分类、标签与友链的状态、数据访问及下列显式依赖。
type Service struct {
	db  *sql.DB
	cfg platform.Config
	now func() time.Time
}

// 用途：构造分类、标签与友链，由 app 唯一装配依赖。
func New(db *sql.DB, cfg platform.Config, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.TimeZone == nil {
		cfg.TimeZone = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return &Service{db: db, cfg: cfg, now: now}
}

// 用途：文章保存复用名称查重，在调用方事务内建立唯一分类和标签关联。
func (s *Service) ResolveNamesTx(ctx context.Context, tx *sql.Tx, category string, tags []string) (*int64, []int64, error) {
	if tx == nil {
		return nil, nil, errors.New("name resolution requires the caller transaction")
	}
	category = strings.TrimSpace(category)
	if category != "" {
		if err := nameValid(category); err != nil {
			return nil, nil, err
		}
	}
	normalized := make([]string, len(tags))
	for index, tag := range tags {
		normalized[index] = strings.TrimSpace(tag)
		if err := nameValid(normalized[index]); err != nil {
			return nil, nil, err
		}
	}
	var categoryID *int64
	if category != "" {
		id, err := s.resolveName(ctx, tx, categoryKind, category)
		if err != nil {
			return nil, nil, err
		}
		categoryID = &id
	}
	tagIDs := make([]int64, 0, len(normalized))
	seen := make(map[int64]bool)
	for _, tag := range normalized {
		id, err := s.resolveName(ctx, tx, tagKind, tag)
		if err != nil {
			return nil, nil, err
		}
		// SQL NOCASE is authoritative: it folds ASCII without changing Unicode names.
		if !seen[id] {
			seen[id] = true
			tagIDs = append(tagIDs, id)
		}
	}
	return categoryID, tagIDs, nil
}

// 用途：承接原 GET /categories 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListCategories(ctx context.Context, in platform.Input) (any, error) {
	records, err := s.readNames(ctx, s.db, categoryKind, "public", "", 0, 0)
	return platform.Page{RecordList: records, Count: int64(len(records))}, err
}

// 用途：承接原 GET /admin/categories 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListBackCategories(ctx context.Context, in platform.Input) (any, error) {
	return s.listNamesPage(ctx, in, categoryKind)
}

// 用途：承接原 GET /admin/categories/search 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListCategoriesBySearch(ctx context.Context, in platform.Input) (any, error) {
	return s.readNames(ctx, s.db, categoryKind, "search", in.Query.Get("keywords"), 0, 0)
}

// 用途：承接原 POST /admin/categories 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveOrUpdateCategory(ctx context.Context, in platform.Input) (any, error) {
	return s.saveName(ctx, in, categoryKind)
}

// 用途：承接原 DELETE /admin/categories 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeleteCategories(ctx context.Context, in platform.Input) (any, error) {
	return s.deleteRecords(ctx, in, "tb_category", "tb_article", "category_id", "分类")
}

// 用途：承接原 GET /links 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListFriendLinks(ctx context.Context, in platform.Input) (any, error) {
	return s.readLinks(ctx, s.db, false, "", 0, 0)
}

// 用途：承接原 GET /admin/links 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListFriendLinkDTO(ctx context.Context, in platform.Input) (any, error) {
	current, size, err := pagination(in.Query)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	keyword := in.Query.Get("keywords")
	page := platform.Page{RecordList: []platform.Object{}}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM tb_friend_link WHERE link_name LIKE ? ESCAPE '\\'", searchPattern(keyword)).Scan(&page.Count); err != nil {
		return nil, err
	}
	if page.Count > 0 && current-1 <= (page.Count-1)/size {
		page.RecordList, err = s.readLinks(ctx, tx, true, keyword, size, (current-1)*size)
		if err != nil {
			return nil, err
		}
	}
	return page, tx.Commit()
}

// 用途：承接原 POST /admin/links 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveOrUpdateFriendLink(ctx context.Context, in platform.Input) (any, error) {
	var request struct {
		ID                                           *int64
		LinkName, LinkAvatar, LinkAddress, LinkIntro string
	}
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	request.LinkName = strings.TrimSpace(request.LinkName)
	request.LinkAvatar = strings.TrimSpace(request.LinkAvatar)
	request.LinkAddress = strings.TrimSpace(request.LinkAddress)
	request.LinkIntro = strings.TrimSpace(request.LinkIntro)
	if !textValid(request.LinkName, 20) || !textValid(request.LinkIntro, 100) ||
		!httpURLValid(request.LinkAvatar, 255) || !httpURLValid(request.LinkAddress, 50) ||
		(request.ID != nil && *request.ID <= 0) {
		return nil, invalid("友链字段不正确")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stamp := s.now().In(s.cfg.TimeZone).Format("2006-01-02T15:04:05")
	if request.ID == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO tb_friend_link(link_name,link_avatar,link_address,link_intro,create_time) VALUES(?,?,?,?,?)",
			request.LinkName, request.LinkAvatar, request.LinkAddress, request.LinkIntro, stamp)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "UPDATE tb_friend_link SET link_name=?,link_avatar=?,link_address=?,link_intro=?,update_time=? WHERE id=?",
			request.LinkName, request.LinkAvatar, request.LinkAddress, request.LinkIntro, stamp, *request.ID)
		if err == nil {
			var affected int64
			affected, err = result.RowsAffected()
			if err == nil && affected == 0 {
				return nil, failure("友链不存在")
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 DELETE /admin/links 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeleteFriendLink(ctx context.Context, in platform.Input) (any, error) {
	return s.deleteRecords(ctx, in, "tb_friend_link", "", "", "友链")
}

// 用途：承接原 GET /tags 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListTags(ctx context.Context, in platform.Input) (any, error) {
	records, err := s.readNames(ctx, s.db, tagKind, "public", "", 0, 0)
	return platform.Page{RecordList: records, Count: int64(len(records))}, err
}

// 用途：承接原 GET /admin/tags 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListTagBackDTO(ctx context.Context, in platform.Input) (any, error) {
	return s.listNamesPage(ctx, in, tagKind)
}

// 用途：承接原 GET /admin/tags/search 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListTagsBySearch(ctx context.Context, in platform.Input) (any, error) {
	return s.readNames(ctx, s.db, tagKind, "search", in.Query.Get("keywords"), 0, 0)
}

// 用途：承接原 POST /admin/tags 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveOrUpdateTag(ctx context.Context, in platform.Input) (any, error) {
	return s.saveName(ctx, in, tagKind)
}

// 用途：承接原 DELETE /admin/tags 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeleteTag(ctx context.Context, in platform.Input) (any, error) {
	return s.deleteRecords(ctx, in, "tb_tag", "tb_article_tag", "tag_id", "标签")
}

// Identifiers below are fixed module declarations and never come from a request.
type namedKind struct {
	table, column, field, label string
}

var categoryKind = namedKind{"tb_category", "category_name", "categoryName", "分类"}
var tagKind = namedKind{"tb_tag", "tag_name", "tagName", "标签"}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func invalid(message string) error {
	return &platform.Error{Code: platform.CodeInvalid, Message: message}
}

func failure(message string) error {
	return &platform.Error{Code: platform.CodeFail, Message: message}
}

func textValid(value string, limit int) bool {
	return value != "" && utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= limit
}

func nameValid(name string) error {
	if !textValid(name, 20) {
		return invalid("名称不能为空且不能超过20个字符")
	}
	return nil
}

func objectInput(body json.RawMessage, destination any) error {
	value := strings.TrimSpace(string(body))
	if !strings.HasPrefix(value, "{") || json.Unmarshal(body, destination) != nil {
		return invalid("请求字段不正确")
	}
	return nil
}

func httpURLValid(value string, limit int) bool {
	if !textValid(value, limit) {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Hostname() != "" && parsed.User == nil
}

func pagination(query url.Values) (int64, int64, error) {
	values := [2]int64{1, 10}
	for index, field := range []string{"current", "size"} {
		if raw, exists := query[field]; exists {
			if len(raw) != 1 {
				return 0, 0, invalid("分页参数不正确")
			}
			value, err := strconv.ParseInt(raw[0], 10, 64)
			if err != nil || value < 1 || (field == "size" && value > 100) {
				return 0, 0, invalid("分页参数不正确")
			}
			values[index] = value
		}
	}
	return values[0], values[1], nil
}

func searchPattern(keyword string) string {
	return "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.TrimSpace(keyword)) + "%"
}

func (s *Service) localTime(value string) (string, error) {
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05", time.RFC3339Nano} {
		if parsed, err := time.ParseInLocation(layout, value, s.cfg.TimeZone); err == nil {
			return parsed.In(s.cfg.TimeZone).Format("2006-01-02T15:04:05"), nil
		}
	}
	return "", errors.New("invalid stored catalog creation time")
}

func (s *Service) resolveName(ctx context.Context, tx *sql.Tx, kind namedKind, name string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, "SELECT id FROM "+kind.table+" WHERE "+kind.column+"=? COLLATE NOCASE", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO "+kind.table+"("+kind.column+",create_time) VALUES(?,?)",
		name, s.now().In(s.cfg.TimeZone).Format("2006-01-02T15:04:05"))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Service) saveName(ctx context.Context, in platform.Input, kind namedKind) (any, error) {
	var request struct {
		ID                    *int64
		CategoryName, TagName string
	}
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	name := request.CategoryName
	if kind.table == "tb_tag" {
		name = request.TagName
	}
	name = strings.TrimSpace(name)
	if err := nameValid(name); err != nil {
		return nil, err
	}
	if request.ID != nil && *request.ID <= 0 {
		return nil, invalid("ID必须为正整数")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if request.ID == nil {
		// Creation and article resolution share the same case-insensitive identity.
		if _, err := s.resolveName(ctx, tx, kind, name); err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	var present int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM "+kind.table+" WHERE id=?", *request.ID).Scan(&present); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, failure(kind.label + "不存在")
		}
		return nil, err
	}
	var duplicateID int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM "+kind.table+" WHERE "+kind.column+"=? COLLATE NOCASE", name).Scan(&duplicateID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && duplicateID != *request.ID {
		// Editing does not silently move articles between two existing identities.
		return nil, failure(kind.label + "名已存在")
	}
	_, err = tx.ExecContext(ctx, "UPDATE "+kind.table+" SET "+kind.column+"=?,update_time=? WHERE id=?",
		name, s.now().In(s.cfg.TimeZone).Format("2006-01-02T15:04:05"), *request.ID)
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

func (s *Service) listNamesPage(ctx context.Context, in platform.Input, kind namedKind) (any, error) {
	current, size, err := pagination(in.Query)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	keyword := in.Query.Get("keywords")
	page := platform.Page{RecordList: []platform.Object{}}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+kind.table+" WHERE "+kind.column+" LIKE ? ESCAPE '\\'", searchPattern(keyword)).Scan(&page.Count); err != nil {
		return nil, err
	}
	// Bound the page index before multiplication, including MaxInt64 page requests.
	if page.Count > 0 && current-1 <= (page.Count-1)/size {
		page.RecordList, err = s.readNames(ctx, tx, kind, "admin", keyword, size, (current-1)*size)
		if err != nil {
			return nil, err
		}
	}
	return page, tx.Commit()
}

func (s *Service) readNames(ctx context.Context, db queryer, kind namedKind, mode, keyword string, limit, offset int64) ([]platform.Object, error) {
	withCount := mode == "admin" || (mode == "public" && kind.table == "tb_category")
	withTime := mode == "admin"
	query := "SELECT n.id,n." + kind.column
	if withCount {
		query += ",COUNT(a.id)"
	}
	if withTime {
		query += ",n.create_time"
	}
	query += " FROM " + kind.table + " n"
	if withCount {
		if kind.table == "tb_category" {
			query += " LEFT JOIN tb_article a ON a.category_id=n.id"
			if mode == "public" {
				query += " AND a.status=1 AND a.is_delete=0"
			}
		} else {
			query += " LEFT JOIN tb_article_tag a ON a.tag_id=n.id"
		}
	}
	args := []any{}
	if mode != "public" {
		query += " WHERE n." + kind.column + " LIKE ? ESCAPE '\\'"
		args = append(args, searchPattern(keyword))
	}
	if withCount {
		query += " GROUP BY n.id"
	}
	query += " ORDER BY n.id"
	if mode != "public" {
		query += " DESC"
	}
	if withTime {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]platform.Object, 0)
	for rows.Next() {
		var id, count int64
		var name, created string
		targets := []any{&id, &name}
		if withCount {
			targets = append(targets, &count)
		}
		if withTime {
			targets = append(targets, &created)
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		record := platform.Object{"id": id, kind.field: name}
		if withCount {
			record["articleCount"] = count
		}
		if withTime {
			created, err = s.localTime(created)
			if err != nil {
				return nil, err
			}
			record["createTime"] = created
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Service) readLinks(ctx context.Context, db queryer, admin bool, keyword string, limit, offset int64) ([]platform.Object, error) {
	query := "SELECT id,link_name,link_avatar,link_address,link_intro"
	if admin {
		query += ",create_time"
	}
	query += " FROM tb_friend_link"
	args := []any{}
	if admin {
		query += " WHERE link_name LIKE ? ESCAPE '\\'"
		args = append(args, searchPattern(keyword))
	}
	query += " ORDER BY id"
	if admin {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]platform.Object, 0)
	for rows.Next() {
		var id int64
		var name, avatar, address, intro, created string
		targets := []any{&id, &name, &avatar, &address, &intro}
		if admin {
			targets = append(targets, &created)
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		record := platform.Object{"id": id, "linkName": name, "linkAvatar": avatar, "linkAddress": address, "linkIntro": intro}
		if admin {
			created, err = s.localTime(created)
			if err != nil {
				return nil, err
			}
			record["createTime"] = created
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Service) deleteRecords(ctx context.Context, in platform.Input, table, relationTable, relationColumn, label string) (any, error) {
	var raw []int64
	if json.Unmarshal(in.Body, &raw) != nil || len(raw) == 0 {
		return nil, invalid("请选择要删除的记录")
	}
	ids := make([]int64, 0, len(raw))
	seen := make(map[int64]bool)
	for _, id := range raw {
		if id <= 0 {
			return nil, invalid("ID必须为正整数")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Bounded batches avoid SQLite parameter limits without one query per row.
	const batchSize = 500
	for start := 0; start < len(ids); start += batchSize {
		end := min(start+batchSize, len(ids))
		parameters, args := idParameters(ids[start:end])
		var count int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id IN ("+parameters+")", args...).Scan(&count); err != nil {
			return nil, err
		}
		if count != int64(end-start) {
			return nil, failure(label + "不存在")
		}
		if relationTable != "" {
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+relationTable+" WHERE "+relationColumn+" IN ("+parameters+")", args...).Scan(&count); err != nil {
				return nil, err
			}
			if count > 0 {
				return nil, failure("删除失败，该" + label + "下存在文章")
			}
		}
	}
	for start := 0; start < len(ids); start += batchSize {
		parameters, args := idParameters(ids[start:min(start+batchSize, len(ids))])
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id IN ("+parameters+")", args...); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}

func idParameters(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

// 用途：将本模块 schema.sql 交给唯一迁移器，业务逻辑不自动建表。
//
//go:embed schema.sql
var Schema string

// Public protocol projections use platform.Object; no database row is serialized directly.
// Integer/Long: JSON number; Date: Unix milliseconds; LocalDateTime: yyyy-MM-ddTHH:mm:ss.
// Optional fields retain null; lists/sets are arrays. UserDetailDTO never includes password.
// CategoryDTO: id:Integer, categoryName:String, articleCount:Integer
// CategoryBackDTO: id:Integer, categoryName:String, articleCount:Integer, createTime:LocalDateTime
// CategoryOptionDTO: id:Integer, categoryName:String
// TagDTO: id:Integer, tagName:String
// TagBackDTO: id:Integer, tagName:String, articleCount:Integer, createTime:LocalDateTime
// FriendLinkDTO: id:Integer, linkName:String, linkAvatar:String, linkAddress:String, linkIntro:String
// FriendLinkBackDTO: id:Integer, linkName:String, linkAvatar:String, linkAddress:String, linkIntro:String, createTime:LocalDateTime
// CategoryVO: id:Integer, categoryName:String
// TagVO: id:Integer, tagName:String
// FriendLinkVO: id:Integer, linkName:String, linkAvatar:String, linkAddress:String, linkIntro:String
