package analytics

import (
	"context"
	"crypto/md5"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"blog-go/internal/platform"
)

// Service owns persistent visitor facts, operation logs and optional IP enrichment.
type Service struct {
	db     *sql.DB
	cfg    platform.Config
	now    func() time.Time
	client *http.Client
}

func New(db *sql.DB, cfg platform.Config, client *http.Client, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	bounded := http.Client{}
	if client != nil {
		bounded = *client
	}
	if bounded.Timeout <= 0 || bounded.Timeout > 2*time.Second {
		bounded.Timeout = 2 * time.Second
	}
	// The deployer selected one provider endpoint; redirects must not forward the visitor IP elsewhere.
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	values := make(map[string]string, len(cfg.Values))
	for key, value := range cfg.Values {
		values[key] = value
	}
	cfg.Values = values
	return &Service{db: db, cfg: cfg, client: &bounded, now: now}
}

var visitLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return location
}()

func (s *Service) Audit(ctx context.Context, event platform.AuditEvent) error {
	spec := event.Route
	metadata, allowed := auditActions[spec.Method+" "+spec.Path]
	if !allowed || !spec.Audit || spec.Policy != "admin" || spec.Operation != metadata.operation ||
		event.Code != platform.CodeOK || !event.Actor.LoggedIn || event.Actor.AuthID <= 0 || event.Actor.UserInfoID <= 0 {
		return nil
	}
	if event.Count < 0 {
		return invalidInput()
	}
	ids := make([]int64, 0, len(event.ObjectIDs))
	seen := make(map[int64]bool)
	for _, id := range event.ObjectIDs {
		if id <= 0 {
			return invalidInput()
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	request, err := json.Marshal(platform.Object{"objectIds": ids, "count": event.Count})
	if err != nil {
		return err
	}
	response, err := json.Marshal(platform.Object{"code": event.Code})
	if err != nil {
		return err
	}
	at := event.At
	if at.IsZero() {
		at = s.now()
	}
	source := s.LookupIP(ctx, event.IP)
	_, err = s.db.ExecContext(ctx, `INSERT INTO tb_operation_log
  (opt_module,opt_type,opt_url,opt_method,opt_desc,request_param,request_method,response_data,user_id,nickname,ip_address,ip_source,create_time)
  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, metadata.module, metadata.kind, spec.Path, metadata.method, metadata.description,
		string(request), spec.Method, string(response), event.Actor.AuthID, event.Actor.Nickname, event.IP, source, at.In(visitLocation).Format("2006-01-02T15:04:05"))
	if err != nil {
		return fmt.Errorf("write operation log: %w", err)
	}
	return nil
}

// ReconcileDays repairs the compatibility aggregate from facts without discarding any visitor history.
func (s *Service) ReconcileDays(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE tb_unique_view SET views_count=(SELECT count(*) FROM tb_visit_record WHERE visit_date=substr(tb_unique_view.create_time,1,10))`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tb_unique_view(views_count,create_time)
  SELECT count(*),visit_date || 'T00:00:00' FROM tb_visit_record WHERE 1 GROUP BY visit_date
  ON CONFLICT(substr(create_time,1,10)) DO UPDATE SET views_count=excluded.views_count`); err != nil {
		return err
	}
	if err = upsertDay(ctx, tx, now.In(visitLocation).Format("2006-01-02")); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertDay(ctx context.Context, tx *sql.Tx, day string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO tb_unique_view(views_count,create_time)
  VALUES((SELECT count(*) FROM tb_visit_record WHERE visit_date=?),?)
  ON CONFLICT(substr(create_time,1,10)) DO UPDATE SET views_count=excluded.views_count`, day, day+"T00:00:00")
	return err
}

func (s *Service) GetBlogBackInfo(ctx context.Context, in platform.Input) (any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := platform.Object{}
	for _, counter := range []struct{ field, query string }{
		{"viewsCount", "SELECT count(*) FROM tb_visit_record"},
		{"messageCount", "SELECT count(*) FROM tb_message"},
		{"userCount", "SELECT count(*) FROM tb_user_info"},
		{"articleCount", "SELECT count(*) FROM tb_article WHERE is_delete=0"},
	} {
		var count int64
		if err := tx.QueryRowContext(ctx, counter.query).Scan(&count); err != nil {
			return nil, err
		}
		result[counter.field] = count
	}
	for _, projection := range []struct {
		field, query string
		keys         []string
	}{
		{"categoryDTOList", `SELECT c.id,c.category_name,count(a.id) FROM tb_category c LEFT JOIN tb_article a ON a.category_id=c.id AND a.is_delete=0 AND a.status=1 GROUP BY c.id ORDER BY c.id`, []string{"id", "categoryName", "articleCount"}},
		{"tagDTOList", `SELECT id,tag_name FROM tb_tag ORDER BY id`, []string{"id", "tagName"}},
		{"articleStatisticsList", `SELECT substr(create_time,1,10),count(*) FROM tb_article GROUP BY substr(create_time,1,10) ORDER BY substr(create_time,1,10)`, []string{"date", "count"}},
		{"articleRankDTOList", `SELECT a.article_title,count(*) FROM tb_article_view v JOIN tb_article a ON a.id=v.article_id GROUP BY a.id ORDER BY count(*) DESC,a.id LIMIT 5`, []string{"articleTitle", "viewsCount"}},
	} {
		rows, err := queryObjects(ctx, tx, projection.query, projection.keys)
		if err != nil {
			return nil, err
		}
		result[projection.field] = rows
	}
	today := s.now().In(visitLocation)
	first := today.AddDate(0, 0, -7)
	counts, err := queryObjects(ctx, tx, `SELECT visit_date,count(*) FROM tb_visit_record WHERE visit_date BETWEEN ? AND ? GROUP BY visit_date`, []string{"day", "viewsCount"}, first.Format("2006-01-02"), today.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	byDay := make(map[string]int64, len(counts))
	for _, row := range counts {
		byDay[row["day"].(string)] = row["viewsCount"].(int64)
	}
	trend := make([]platform.Object, 0, 8)
	for offset := 0; offset < 8; offset++ {
		day := first.AddDate(0, 0, offset).Format("2006-01-02")
		trend = append(trend, platform.Object{"day": day, "viewsCount": byDay[day]})
	}
	result["uniqueViewDTOList"] = trend
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) Report(ctx context.Context, in platform.Input) (any, error) {
	if net.ParseIP(in.IP) == nil {
		return nil, invalidInput()
	}
	if in.Browser == "" {
		in.Browser = "Unknown"
	}
	if in.OS == "" {
		in.OS = "Unknown"
	}
	at := s.now().In(visitLocation)
	day := at.Format("2006-01-02")
	digest := md5.Sum([]byte(in.IP + in.Browser + in.OS))
	key := hex.EncodeToString(digest[:])
	// Keep external I/O outside the write transaction and avoid repeated provider calls for known visitors.
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tb_visit_record WHERE visit_date=? AND visitor_key=?)`, day, key).Scan(&exists); err != nil {
		return nil, err
	}
	source := ""
	if !exists {
		source = s.LookupIP(ctx, in.IP)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO tb_visit_record(visit_date,visitor_key,ip_source,create_time) VALUES(?,?,?,?) ON CONFLICT(visit_date,visitor_key) DO NOTHING`, day, key, source, at.Format("2006-01-02T15:04:05")); err != nil {
		return nil, err
	}
	if err = upsertDay(ctx, tx, day); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

func (s *Service) ListOperationLogs(ctx context.Context, in platform.Input) (any, error) {
	size, offset, err := pageWindow(in.Query)
	if err != nil {
		return nil, err
	}
	where := ""
	args := []any{}
	if keyword := strings.TrimSpace(in.Query.Get("keywords")); keyword != "" {
		where = " WHERE (instr(opt_module,?)>0 OR instr(opt_desc,?)>0)"
		args = append(args, keyword, keyword)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	page := platform.Page{RecordList: []platform.Object{}}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM tb_operation_log"+where, args...).Scan(&page.Count); err != nil {
		return nil, err
	}
	args = append(args, size, offset)
	page.RecordList, err = queryObjects(ctx, tx, `SELECT id,opt_module,opt_url,opt_type,opt_method,opt_desc,request_method,request_param,response_data,nickname,ip_address,ip_source,create_time FROM tb_operation_log`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, []string{"id", "optModule", "optUrl", "optType", "optMethod", "optDesc", "requestMethod", "requestParam", "responseData", "nickname", "ipAddress", "ipSource", "createTime"}, args...)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *Service) DeleteOperationLogs(ctx context.Context, in platform.Input) (any, error) {
	var ids []int64
	if err := json.Unmarshal(in.Body, &ids); err != nil || len(ids) == 0 {
		return nil, invalidInput()
	}
	unique := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, invalidInput()
		}
		unique[id] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, "DELETE FROM tb_operation_log WHERE id=?")
	if err != nil {
		return nil, err
	}
	defer statement.Close()
	for id := range unique {
		if _, err := statement.ExecContext(ctx, id); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}

func (s *Service) ListUserAreas(ctx context.Context, in platform.Input) (any, error) {
	query := `SELECT coalesce(ip_source,''),count(*) FROM tb_user_auth GROUP BY coalesce(ip_source,'')`
	args := []any{}
	switch in.Query.Get("type") {
	case "1":
	case "2":
		query = `SELECT ip_source,count(*) FROM tb_visit_record WHERE visit_date=? GROUP BY ip_source`
		args = append(args, s.now().In(visitLocation).Format("2006-01-02"))
	default:
		return nil, invalidInput()
	}
	rows, err := queryObjects(ctx, s.db, query, []string{"source", "count"}, args...)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int64)
	for _, row := range rows {
		counts[areaName(row["source"].(string))] += row["count"].(int64)
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]platform.Object, 0, len(names))
	for _, name := range names {
		result = append(result, platform.Object{"name": name, "count": counts[name]})
	}
	return result, nil
}

func areaName(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "未知"
	}
	// Preserve the original map's two-character region projection, safely handling short Unicode names.
	characters := []rune(source)
	if len(characters) > 2 {
		characters = characters[:2]
	}
	return strings.NewReplacer("省", "", "市", "").Replace(string(characters))
}

func invalidInput() error {
	return &platform.Error{Code: platform.CodeInvalid, Message: "请求参数不正确"}
}

func pageWindow(query url.Values) (size, offset int64, err error) {
	current := int64(1)
	size = 10
	for _, field := range []string{"current", "size"} {
		if values, present := query[field]; present {
			if len(values) != 1 {
				return 0, 0, invalidInput()
			}
			value, e := strconv.ParseInt(values[0], 10, 64)
			if e != nil || value < 1 || (field == "size" && value > 100) {
				return 0, 0, invalidInput()
			}
			if field == "current" {
				current = value
			} else {
				size = value
			}
		}
	}
	if current-1 > math.MaxInt64/size {
		return 0, 0, invalidInput()
	}
	return size, (current - 1) * size, nil
}

type rowQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Each caller supplies its frozen public projection; database rows never escape unchanged.
func queryObjects(ctx context.Context, db rowQuery, query string, keys []string, args ...any) ([]platform.Object, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []platform.Object{}
	for rows.Next() {
		values := make([]any, len(keys))
		dest := make([]any, len(keys))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		item := platform.Object{}
		for i, key := range keys {
			item[key] = values[i]
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// 用途：将本模块 schema.sql 交给唯一迁移器，业务逻辑不自动建表。
//
//go:embed schema.sql
var Schema string

// Public protocol projections use platform.Object; no database row is serialized directly.
// Integer/Long: JSON number; Date: Unix milliseconds; LocalDateTime: yyyy-MM-ddTHH:mm:ss.
// Optional fields retain null; lists/sets are arrays. UserDetailDTO never includes password.
// BlogBackInfoDTO: viewsCount:Integer, messageCount:Integer, userCount:Integer, articleCount:Integer, categoryDTOList:CategoryDTO[], tagDTOList:TagDTO[], articleStatisticsList:ArticleStatisticsDTO[], uniqueViewDTOList:UniqueViewDTO[], articleRankDTOList:ArticleRankDTO[]
// ArticleRankDTO: articleTitle:String, viewsCount:Integer
// ArticleStatisticsDTO: date:String, count:Integer
// UniqueViewDTO: day:String, viewsCount:Integer
// OperationLogDTO: id:Integer, optModule:String, optUrl:String, optType:String, optMethod:String, optDesc:String, requestMethod:String, requestParam:String, responseData:String, nickname:String, ipAddress:String, ipSource:String, createTime:LocalDateTime
// UserAreaDTO: name:String, count:Long
