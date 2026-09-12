package article

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"blog-go/internal/catalog"
	"blog-go/internal/engagement"
	"blog-go/internal/files"
	"blog-go/internal/platform"
	"blog-go/internal/site"
)

// 用途：拥有文章、检索与文章文件交换的状态、数据访问及下列显式依赖。
type Service struct {
	db         *sql.DB
	cfg        platform.Config
	now        func() time.Time
	catalog    *catalog.Service
	site       *site.Service
	files      *files.Service
	engagement *engagement.Service
}

// 用途：构造文章、检索与文章文件交换，由 app 唯一装配依赖。
func New(db *sql.DB, cfg platform.Config, catalog *catalog.Service, site *site.Service, files *files.Service, engagement *engagement.Service, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.TimeZone == nil {
		cfg.TimeZone = time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return &Service{db: db, cfg: cfg, catalog: catalog, site: site, files: files, engagement: engagement, now: now}
}

// 用途：承接原 GET /articles/archives 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListArchives(ctx context.Context, in platform.Input) (any, error) {
	return s.listPage(ctx, in, "archive")
}

// 用途：承接原 GET /articles 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListArticles(ctx context.Context, in platform.Input) (any, error) {
	page, err := s.listPage(ctx, in, "home")
	return page.RecordList, err
}

// 用途：承接原 GET /admin/articles 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListArticleBacks(ctx context.Context, in platform.Input) (any, error) {
	return s.listPage(ctx, in, "back")
}

// 用途：承接原 POST /admin/articles 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveOrUpdateArticle(ctx context.Context, in platform.Input) (any, error) {
	var request articleInput
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	if err := validateArticle(request); err != nil {
		return nil, err
	}
	if !in.Actor.LoggedIn || in.Actor.UserInfoID <= 0 {
		return nil, failure("请先登录")
	}
	if err := s.defaultCover(ctx, &request); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.saveTx(ctx, tx, in.Actor.UserInfoID, request); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 PUT /admin/articles/top 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateArticleTop(ctx context.Context, in platform.Input) (any, error) {
	var request struct {
		ID    int64
		IsTop *int
	}
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	if request.ID <= 0 || request.IsTop == nil || *request.IsTop < 0 || *request.IsTop > 1 {
		return nil, invalid("置顶参数不正确")
	}
	result, err := s.db.ExecContext(ctx, "UPDATE tb_article SET is_top=?,update_time=? WHERE id=? AND is_delete=0", *request.IsTop, s.stamp(), request.ID)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return nil, failure("文章不存在或已删除")
	}
	return nil, err
}

// 用途：承接原 PUT /admin/articles 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateArticleDelete(ctx context.Context, in platform.Input) (any, error) {
	var request struct {
		IDList   []int64
		IsDelete *int
	}
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	ids, err := uniqueIDs(request.IDList)
	if err != nil {
		return nil, err
	}
	if request.IsDelete == nil || *request.IsDelete < 0 || *request.IsDelete > 1 {
		return nil, invalid("删除状态不正确")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := ensureArticles(ctx, tx, ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, "UPDATE tb_article SET is_delete=?,is_top=0,update_time=? WHERE id=?", *request.IsDelete, s.stamp(), id); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}

// 用途：承接原 DELETE /admin/articles 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeleteArticles(ctx context.Context, in platform.Input) (any, error) {
	ids, err := bodyIDs(in.Body)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := ensureArticles(ctx, tx, ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		// Include descendants even when an old reply has an inconsistent topic_id.
		rows, err := tx.QueryContext(ctx, `WITH RECURSIVE comments(id) AS (
		 SELECT id FROM tb_comment WHERE type=1 AND topic_id=?
		 UNION SELECT c.id FROM tb_comment c JOIN comments p ON c.parent_id=p.id
		) SELECT id FROM comments`, id)
		if err != nil {
			return nil, err
		}
		comments := []int64{}
		for rows.Next() {
			var commentID int64
			if err = rows.Scan(&commentID); err != nil {
				rows.Close()
				return nil, err
			}
			comments = append(comments, commentID)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if err = s.engagement.DeleteTargetsTx(ctx, tx, 2, comments); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM tb_comment WHERE type=1 AND topic_id=?", id); err != nil {
			return nil, err
		}
		if err = s.engagement.DeleteTargetsTx(ctx, tx, 1, []int64{id}); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM tb_article WHERE id=?", id); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}

// 用途：承接原 GET /admin/articles/{articleId} 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) GetArticleBackById(ctx context.Context, in platform.Input) (any, error) {
	id, err := articleID(in)
	if err != nil {
		return nil, err
	}
	rows, err := s.readArticles(ctx, s.db, "a.id=?", []any{id}, "a.id", 1, 0, "edit")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, failure("文章不存在")
	}
	r := rows[0]
	tags := make([]string, 0, len(r.tags))
	for _, tag := range r.tags {
		tags = append(tags, tag["tagName"].(string))
	}
	return platform.Object{"id": r.id, "articleTitle": r.title, "articleContent": r.content, "articleCover": nullableString(r.cover), "categoryName": nullableString(r.categoryName), "tagNameList": tags, "type": r.typ, "originalUrl": nullableString(r.originalURL), "isTop": r.top, "status": r.status}, nil
}

// 用途：承接原 GET /articles/{articleId} 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) GetArticleById(ctx context.Context, in platform.Input) (any, error) {
	id, err := articleID(in)
	if err != nil {
		return nil, err
	}
	_, err = s.engagement.RecordView(ctx, id, in.VisitorKey)
	if err != nil {
		var e *platform.Error
		if errors.As(err, &e) && e.Code == platform.CodeMissing {
			return nil, errors.Join(failure("文章不存在或不可见"), err)
		}
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := s.readArticles(ctx, tx, publicArticles+" AND a.id=?", []any{id}, "a.id", 1, 0, "detail")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, failure("文章不存在或不可见")
	}
	r := rows[0]
	result := r.projection("detail")
	for _, neighbor := range []struct{ key, condition, order string }{{"lastArticle", "a.id<?", "a.id DESC"}, {"nextArticle", "a.id>?", "a.id ASC"}} {
		items, err := s.readArticles(ctx, tx, publicArticles+" AND "+neighbor.condition, []any{id}, neighbor.order, 1, 0, "neighbor")
		if err != nil {
			return nil, err
		}
		// BeanCopyUtils creates an empty ArticlePaginationDTO for a missing
		// neighbor; the original Vue page dereferences its id unconditionally.
		result[neighbor.key] = platform.Object{"id": nil, "articleTitle": nil, "articleCover": nil}
		if len(items) > 0 {
			result[neighbor.key] = items[0].projection("neighbor")
		}
	}
	for _, group := range []struct {
		key, condition, order string
		args                  []any
		limit                 int64
	}{
		{"recommendArticleList", publicArticles + ` AND a.id<>? AND EXISTS(SELECT 1 FROM tb_article_tag source JOIN tb_article_tag shared ON source.tag_id=shared.tag_id WHERE source.article_id=? AND shared.article_id=a.id)`, "a.is_top DESC,a.create_time DESC,a.id DESC", []any{id, id}, 6},
		{"newestArticleList", publicArticles, "a.create_time DESC,a.id DESC", nil, 5},
	} {
		items, err := s.readArticles(ctx, tx, group.condition, group.args, group.order, group.limit, 0, "recommend")
		if err != nil {
			return nil, err
		}
		list := make([]platform.Object, 0, len(items))
		for _, item := range items {
			list = append(list, item.projection("recommend"))
		}
		result[group.key] = list
	}
	return result, tx.Commit()
}

// 用途：承接原 GET /articles/condition 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListArticlesByCondition(ctx context.Context, in platform.Input) (any, error) {
	current, size, err := pagination(in.Query)
	if err != nil {
		return nil, err
	}
	where, args, err := filter(in.Query, false)
	if err != nil {
		return nil, err
	}
	key, column, table := "categoryId", "category_name", "tb_category"
	if in.Query.Get(key) == "" {
		key, column, table = "tagId", "tag_name", "tb_tag"
	}
	if in.Query.Get(key) == "" {
		return nil, invalid("分类或标签参数不能为空")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var name any
	var n string
	err = tx.QueryRowContext(ctx, "SELECT "+column+" FROM "+table+" WHERE id=?", in.Query.Get(key)).Scan(&n)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		name = n
	}
	items, _, err := s.pageRows(ctx, tx, where, args, "a.create_time DESC,a.id DESC", current, size, "preview")
	if err != nil {
		return nil, err
	}
	result := make([]platform.Object, 0, len(items))
	for _, r := range items {
		p := r.projection("preview")
		stamp, err := s.parseTime(r.created)
		if err != nil {
			return nil, err
		}
		p["createTime"] = stamp.UnixMilli()
		result = append(result, p)
	}
	return platform.Object{"articlePreviewDTOList": result, "name": name}, tx.Commit()
}

// 用途：承接原 GET /articles/search 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListArticlesBySearch(ctx context.Context, in platform.Input) (any, error) {
	keyword := strings.TrimSpace(in.Query.Get("keywords"))
	result := []platform.Object{}
	if keyword == "" {
		return result, nil
	}
	// SQL matches literal text; HTML is escaped before only our fixed highlight
	// markup is inserted. No user text becomes a regular expression or HTML.
	pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(keyword) + "%"
	rows, err := s.db.QueryContext(ctx, "SELECT id,article_title,article_content FROM tb_article a WHERE "+publicArticles+" AND (article_title LIKE ? ESCAPE '\\' OR article_content LIKE ? ESCAPE '\\') ORDER BY create_time DESC,id DESC", pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var title, content string
		if err := rows.Scan(&id, &title, &content); err != nil {
			return nil, err
		}
		result = append(result, platform.Object{"id": id, "articleTitle": highlight(title, keyword), "articleContent": highlight(searchExcerpt(content, keyword), keyword), "isDelete": 0, "status": 1})
	}
	return result, rows.Err()
}

// 用途：承接原 POST /articles/{articleId}/like 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveArticleLike(ctx context.Context, in platform.Input) (any, error) {
	if !in.Actor.LoggedIn || in.Actor.UserInfoID <= 0 {
		return nil, failure("请先登录")
	}
	id, err := articleID(in)
	if err != nil {
		return nil, err
	}
	_, _, err = s.engagement.Toggle(ctx, in.Actor.UserInfoID, 1, id)
	var business *platform.Error
	if errors.As(err, &business) && business.Code == platform.CodeMissing {
		return nil, errors.Join(failure(business.Message), err)
	}
	return nil, err
}

const publicArticles = "a.status=1 AND a.is_delete=0"

type articleInput struct {
	ID                                      *int64
	ArticleTitle, ArticleContent            string
	ArticleCover, CategoryName, OriginalURL *string
	TagNameList                             []string
	Type, IsTop, Status                     *int
	created                                 *time.Time
}

func invalid(message string) error {
	return &platform.Error{Code: platform.CodeInvalid, Message: message}
}
func failure(message string) error { return &platform.Error{Code: platform.CodeFail, Message: message} }
func objectInput(raw []byte, dst any) error {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil || json.Unmarshal(raw, dst) != nil {
		return invalid("文章参数不正确")
	}
	return nil
}
func value(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func intValue(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}
func nullableString(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}
func nullableInt(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}
func (s *Service) stamp() string { return s.now().In(s.cfg.TimeZone).Format("2006-01-02T15:04:05") }
func (s *Service) parseTime(raw string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05", time.RFC3339Nano} {
		if v, err := time.ParseInLocation(layout, raw, s.cfg.TimeZone); err == nil {
			return v.In(s.cfg.TimeZone), nil
		}
	}
	return time.Time{}, errors.New("invalid stored article time")
}
func articleID(in platform.Input) (int64, error) {
	id, err := strconv.ParseInt(in.Params["articleId"], 10, 64)
	if err != nil || id <= 0 {
		return 0, invalid("文章ID不正确")
	}
	return id, nil
}
func validateArticle(in articleInput) error {
	if !utf8.ValidString(in.ArticleTitle) || !utf8.ValidString(in.ArticleContent) || strings.TrimSpace(in.ArticleTitle) == "" || utf8.RuneCountInString(in.ArticleTitle) > 50 || strings.TrimSpace(in.ArticleContent) == "" || strings.ContainsRune(in.ArticleContent, 0) || strings.ContainsRune(in.ArticleTitle, 0) {
		return invalid("文章标题或正文不正确")
	}
	if in.ID != nil && *in.ID <= 0 || intValue(in.Status, 1) < 1 || intValue(in.Status, 1) > 3 || intValue(in.Type, 1) < 1 || intValue(in.Type, 1) > 3 || intValue(in.IsTop, 0) < 0 || intValue(in.IsTop, 0) > 1 {
		return invalid("文章状态或类型不正确")
	}
	// Match catalog's frozen name contract before any import transaction starts.
	names := append([]string{}, in.TagNameList...)
	if category := strings.TrimSpace(value(in.CategoryName)); category != "" {
		names = append(names, category)
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) || utf8.RuneCountInString(name) > 20 {
			return invalid("分类标签名称不能为空且不能超过20个字符")
		}
	}
	return nil
}
func (s *Service) defaultCover(ctx context.Context, in *articleInput) error {
	if strings.TrimSpace(value(in.ArticleCover)) != "" {
		return nil
	}
	cfg, err := s.site.Config(ctx)
	if err != nil {
		return err
	}
	cover, ok := cfg["articleCover"].(string)
	if !ok || strings.TrimSpace(cover) == "" {
		return failure("默认文章封面未配置")
	}
	in.ArticleCover = &cover
	return nil
}
func (s *Service) saveTx(ctx context.Context, tx *sql.Tx, author int64, in articleInput) error {
	if in.ID != nil {
		if err := ensureArticles(ctx, tx, []int64{*in.ID}); err != nil {
			return err
		}
	}
	var disabled int
	if err := tx.QueryRowContext(ctx, "SELECT is_disable FROM tb_user_info WHERE id=?", author).Scan(&disabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return failure("作者不存在")
		}
		return err
	}
	if disabled != 0 {
		return failure("作者已禁用")
	}
	category, tags, err := s.catalog.ResolveNamesTx(ctx, tx, value(in.CategoryName), in.TagNameList)
	if err != nil {
		return err
	}
	var id int64
	if in.ID == nil {
		created := s.stamp()
		if in.created != nil {
			created = in.created.In(s.cfg.TimeZone).Format("2006-01-02T15:04:05")
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO tb_article(user_id,category_id,article_cover,article_title,article_content,type,original_url,is_top,status,create_time) VALUES(?,?,?,?,?,?,?,?,?,?)", author, category, in.ArticleCover, in.ArticleTitle, in.ArticleContent, intValue(in.Type, 1), in.OriginalURL, intValue(in.IsTop, 0), intValue(in.Status, 1), created)
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		if err != nil {
			return err
		}
	} else {
		id = *in.ID
		_, err = tx.ExecContext(ctx, "UPDATE tb_article SET user_id=?,category_id=?,article_cover=?,article_title=?,article_content=?,type=?,original_url=?,is_top=CASE WHEN is_delete=1 THEN 0 ELSE ? END,status=?,update_time=? WHERE id=?", author, category, in.ArticleCover, in.ArticleTitle, in.ArticleContent, intValue(in.Type, 1), in.OriginalURL, intValue(in.IsTop, 0), intValue(in.Status, 1), s.stamp(), id)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM tb_article_tag WHERE article_id=?", id); err != nil {
			return err
		}
	}
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, "INSERT INTO tb_article_tag(article_id,tag_id) VALUES(?,?)", id, tag); err != nil {
			return err
		}
	}
	return nil
}
func uniqueIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, invalid("文章ID列表不能为空")
	}
	out := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 {
			return nil, invalid("文章ID不正确")
		}
		if !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out, nil
}
func bodyIDs(raw []byte) ([]int64, error) {
	var ids []int64
	if json.Unmarshal(raw, &ids) != nil {
		return nil, invalid("文章ID列表不正确")
	}
	return uniqueIDs(ids)
}
func ensureArticles(ctx context.Context, tx *sql.Tx, ids []int64) error {
	const batchSize = 500
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		args := make([]any, 0, end-start)
		for _, id := range ids[start:end] {
			args = append(args, id)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
		var found int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM tb_article WHERE id IN ("+placeholders+")", args...).Scan(&found); err != nil {
			return err
		}
		if found != len(args) {
			return failure("文章不存在")
		}
	}
	return nil
}
func pagination(q url.Values) (int64, int64, error) {
	values := [2]int64{1, 10}
	for i, k := range []string{"current", "size"} {
		if raw, ok := q[k]; ok {
			if len(raw) != 1 {
				return 0, 0, invalid("分页参数不正确")
			}
			n, err := strconv.ParseInt(raw[0], 10, 64)
			if err != nil || n < 1 || (k == "size" && n > 100) {
				return 0, 0, invalid("分页参数不正确")
			}
			values[i] = n
		}
	}
	return values[0], values[1], nil
}
func filter(q url.Values, admin bool) (string, []any, error) {
	where := publicArticles
	args := []any{}
	if admin {
		where = "a.is_delete=?"
		deleted := int64(0)
		if q.Has("isDelete") {
			n, err := strconv.ParseInt(q.Get("isDelete"), 10, 64)
			if err != nil || n < 0 || n > 1 {
				return "", nil, invalid("删除状态不正确")
			}
			deleted = n
		}
		args = append(args, deleted)
	}
	for _, key := range []string{"categoryId", "tagId", "status", "type"} {
		if !admin && (key == "status" || key == "type") {
			continue
		}
		if q.Has(key) {
			values := q[key]
			if len(values) != 1 {
				return "", nil, invalid("查询条件不正确")
			}
			id, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil || id <= 0 || ((key == "status" || key == "type") && id > 3) {
				return "", nil, invalid("查询条件不正确")
			}
			switch key {
			case "categoryId":
				where += " AND a.category_id=?"
			case "tagId":
				where += " AND EXISTS(SELECT 1 FROM tb_article_tag atg WHERE atg.article_id=a.id AND atg.tag_id=?)"
			case "status":
				where += " AND a.status=?"
			case "type":
				where += " AND a.type=?"
			}
			args = append(args, id)
		}
	}
	if admin && q.Get("keywords") != "" {
		where += " AND a.article_title LIKE ? ESCAPE '\\'"
		args = append(args, "%"+strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.TrimSpace(q.Get("keywords")))+"%")
	}
	return where, args, nil
}

type articleReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type articleRow struct {
	id                                 int64
	cover                              sql.NullString
	title, content, created            string
	updated, originalURL, categoryName sql.NullString
	categoryID                         sql.NullInt64
	typ, top, deleted, status          int
	likes, views                       int64
	tags                               []platform.Object
}

func (s *Service) readArticles(ctx context.Context, db articleReader, where string, args []any, order string, limit, offset int64, kind string) ([]articleRow, error) {
	body := "''"
	if kind == "home" {
		// SQLite substr counts Unicode codepoints; large imported articles do
		// not make the home page retain their complete bodies in memory.
		body = "substr(a.article_content,1,500)"
	} else if kind == "detail" || kind == "edit" {
		body = "a.article_content"
	}
	counts := "0,0"
	if kind == "back" || kind == "detail" {
		counts = `(SELECT count(*) FROM tb_like l WHERE l.target_type=1 AND l.target_id=a.id),(SELECT count(*) FROM tb_article_view v WHERE v.article_id=a.id)`
	}
	query := `SELECT a.id,a.article_cover,a.article_title,` + body + `,a.create_time,a.update_time,a.original_url,a.category_id,c.category_name,a.type,a.is_top,a.is_delete,a.status,` + counts + `,
	 (SELECT json_group_array(json_object('id',t.id,'tagName',t.tag_name)) FROM tb_article_tag atg JOIN tb_tag t ON t.id=atg.tag_id WHERE atg.article_id=a.id)
	 FROM tb_article a LEFT JOIN tb_category c ON a.category_id=c.id WHERE ` + where + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	params := append(append([]any{}, args...), limit, offset)
	rows, err := db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []articleRow{}
	for rows.Next() {
		var r articleRow
		var tags string
		if err := rows.Scan(&r.id, &r.cover, &r.title, &r.content, &r.created, &r.updated, &r.originalURL, &r.categoryID, &r.categoryName, &r.typ, &r.top, &r.deleted, &r.status, &r.likes, &r.views, &tags); err != nil {
			return nil, err
		}
		var values []struct {
			ID      int64
			TagName string
		}
		if err := json.Unmarshal([]byte(tags), &values); err != nil {
			return nil, err
		}
		r.tags = make([]platform.Object, 0, len(values))
		for _, tag := range values {
			r.tags = append(r.tags, platform.Object{"id": tag.ID, "tagName": tag.TagName})
		}
		created, err := s.parseTime(r.created)
		if err != nil {
			return nil, err
		}
		r.created = created.Format("2006-01-02T15:04:05")
		if r.updated.Valid {
			updated, err := s.parseTime(r.updated.String)
			if err != nil {
				return nil, err
			}
			r.updated.String = updated.Format("2006-01-02T15:04:05")
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (r articleRow) projection(kind string) platform.Object {
	p := platform.Object{"id": r.id, "articleTitle": r.title, "articleCover": nullableString(r.cover)}
	if kind == "neighbor" {
		return p
	}
	p["createTime"] = r.created
	if kind == "recommend" {
		return p
	}
	if kind == "archive" {
		delete(p, "articleCover")
		return p
	}
	p["categoryName"] = nullableString(r.categoryName)
	p["tagDTOList"] = r.tags
	if kind == "back" {
		p["likeCount"] = r.likes
		p["viewsCount"] = r.views
		p["type"] = r.typ
		p["isTop"] = r.top
		p["isDelete"] = r.deleted
		p["status"] = r.status
		return p
	}
	p["categoryId"] = nullableInt(r.categoryID)
	if kind == "preview" {
		return p
	}
	p["type"] = r.typ
	p["articleContent"] = r.content
	if kind == "home" {
		p["isTop"] = r.top
		text := []rune(r.content)
		if len(text) > 500 {
			p["articleContent"] = string(text[:500])
		}
		return p
	}
	p["originalUrl"] = nullableString(r.originalURL)
	p["updateTime"] = nullableString(r.updated)
	p["likeCount"] = r.likes
	p["viewsCount"] = r.views
	return p
}
func (s *Service) pageRows(ctx context.Context, db articleReader, where string, args []any, order string, current, size int64, kind string) ([]articleRow, int64, error) {
	var count int64
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM tb_article a WHERE "+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	if count == 0 || current-1 > (count-1)/size {
		return []articleRow{}, count, nil
	}
	rows, err := s.readArticles(ctx, db, where, args, order, size, (current-1)*size, kind)
	return rows, count, err
}
func (s *Service) listPage(ctx context.Context, in platform.Input, kind string) (platform.Page, error) {
	page := platform.Page{RecordList: []platform.Object{}}
	current, size, err := pagination(in.Query)
	if err != nil {
		return page, err
	}
	where, args, err := filter(in.Query, kind == "back")
	if err != nil {
		return page, err
	}
	order := "a.is_top DESC,a.create_time DESC,a.id DESC"
	if kind == "archive" {
		order = "a.create_time DESC,a.id DESC"
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	rows, total, err := s.pageRows(ctx, tx, where, args, order, current, size, kind)
	if err != nil {
		return page, err
	}
	page.Count = total
	for _, r := range rows {
		page.RecordList = append(page.RecordList, r.projection(kind))
	}
	return page, tx.Commit()
}
func highlight(text, keyword string) string {
	var out strings.Builder
	needle := asciiFold(keyword)
	for {
		index := strings.Index(asciiFold(text), needle)
		if index < 0 {
			out.WriteString(html.EscapeString(text))
			break
		}
		out.WriteString(html.EscapeString(text[:index]))
		out.WriteString("<span style='color:#f47466'>")
		out.WriteString(html.EscapeString(text[index : index+len(keyword)]))
		out.WriteString("</span>")
		text = text[index+len(keyword):]
	}
	return out.String()
}

// SQLite LIKE folds ASCII, while preserving non-ASCII codepoints and offsets.
func asciiFold(text string) string {
	bytes := []byte(text)
	for i, b := range bytes {
		if b >= 'A' && b <= 'Z' {
			bytes[i] = b + ('a' - 'A')
		}
	}
	return string(bytes)
}
func searchExcerpt(text, keyword string) string {
	index := strings.Index(asciiFold(text), asciiFold(keyword))
	if index < 0 {
		r := []rune(text)
		if len(r) > 200 {
			return string(r[:200])
		}
		return text
	}
	before := []rune(text[:index])
	after := []rune(text[index+len(keyword):])
	if len(before) > 25 {
		before = before[len(before)-25:]
	}
	if len(after) > 175 {
		after = after[:175]
	}
	return string(before) + text[index:index+len(keyword)] + string(after)
}

// 用途：将本模块 schema.sql 交给唯一迁移器，业务逻辑不自动建表。
//
//go:embed schema.sql
var Schema string

// Public protocol projections use platform.Object; no database row is serialized directly.
// Integer/Long: JSON number; Date: Unix milliseconds; LocalDateTime: yyyy-MM-ddTHH:mm:ss.
// Optional fields retain null; lists/sets are arrays. UserDetailDTO never includes password.
// ArchiveDTO: id:Integer, articleTitle:String, createTime:LocalDateTime
// ArticleBackDTO: id:Integer, articleCover:String, articleTitle:String, createTime:LocalDateTime, likeCount:Integer, viewsCount:Integer, categoryName:String, tagDTOList:TagDTO[], type:Integer, isTop:Integer, isDelete:Integer, status:Integer
// ArticleDTO: id:Integer, articleCover:String, articleTitle:String, articleContent:String, likeCount:Integer, viewsCount:Integer, type:Integer, originalUrl:String, createTime:LocalDateTime, updateTime:LocalDateTime, categoryId:Integer, categoryName:String, tagDTOList:TagDTO[], lastArticle:ArticlePaginationDTO, nextArticle:ArticlePaginationDTO, recommendArticleList:ArticleRecommendDTO[], newestArticleList:ArticleRecommendDTO[]
// ArticleHomeDTO: id:Integer, articleCover:String, articleTitle:String, articleContent:String, createTime:LocalDateTime, isTop:Integer, type:Integer, categoryId:Integer, categoryName:String, tagDTOList:TagDTO[]
// ArticlePaginationDTO: id:Integer, articleCover:String, articleTitle:String
// ArticlePreviewDTO: id:Integer, articleCover:String, articleTitle:String, createTime:Date, categoryId:Integer, categoryName:String, tagDTOList:TagDTO[]
// ArticlePreviewListDTO: articlePreviewDTOList:ArticlePreviewDTO[], name:String
// ArticleRecommendDTO: id:Integer, articleCover:String, articleTitle:String, createTime:LocalDateTime
// ArticleSearchDTO: id:Integer, articleTitle:String, articleContent:String, isDelete:Integer, status:Integer
// ArticleVO: id:Integer, articleTitle:String, articleContent:String, articleCover:String, categoryName:String, tagNameList:String[], type:Integer, originalUrl:String, isTop:Integer, status:Integer
// ArticleTopVO: id:Integer, isTop:Integer
// HexoArticleVO: extends ArticleVO; createTime:LocalDateTime
