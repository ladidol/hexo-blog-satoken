package community

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"blog-go/internal/analytics"
	"blog-go/internal/engagement"
	"blog-go/internal/mail"
	"blog-go/internal/platform"
	"blog-go/internal/site"
)

// 用途：拥有评论、回复与留言审核的状态、数据访问及下列显式依赖。
type Service struct {
	db         *sql.DB
	cfg        platform.Config
	now        func() time.Time
	site       *site.Service
	analytics  *analytics.Service
	mail       *mail.Service
	engagement *engagement.Service
}

// 用途：构造评论、回复与留言审核，由 app 唯一装配依赖。
func New(db *sql.DB, cfg platform.Config, site *site.Service, analytics *analytics.Service, mail *mail.Service, engagement *engagement.Service, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.TimeZone == nil {
		cfg.TimeZone = time.UTC
	}
	return &Service{db: db, cfg: cfg, site: site, analytics: analytics, mail: mail, engagement: engagement, now: now}
}

// 用途：承接原 GET /comments 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListComments(ctx context.Context, in platform.Input) (any, error) {
	kind, topic, err := commentQuery(in.Query)
	if err != nil {
		return nil, err
	}
	current, size, err := pagination(in.Query)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = topicOwner(ctx, tx, kind, topic); err != nil {
		return nil, err
	}
	page := platform.Page{RecordList: []platform.Object{}}
	where := "c.type=? AND c.topic_id IS ? AND c.parent_id IS NULL AND c.is_review=1 AND c.is_delete=0"
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM tb_comment c WHERE "+where, kind, topic).Scan(&page.Count); err != nil {
		return nil, err
	}
	if page.Count == 0 || current-1 > (page.Count-1)/size {
		return page, tx.Commit()
	}
	page.RecordList, err = s.readPublicComments(ctx, tx, where+" ORDER BY c.id DESC LIMIT ? OFFSET ?", []any{kind, topic, size, (current - 1) * size}, false)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(page.RecordList))
	byID := make(map[int64]platform.Object)
	for _, row := range page.RecordList {
		id := row["id"].(int64)
		ids = append(ids, id)
		byID[id] = row
		row["replyCount"] = int64(0)
		row["replyDTOList"] = []platform.Object{}
	}
	if len(ids) > 0 {
		marks, args := idParameters(ids)
		// One window query supplies the first three replies and full approved count
		// for every root in the page, instead of one query for each root.
		query := `WITH ranked AS (SELECT c.id,c.parent_id,row_number() OVER(PARTITION BY c.parent_id ORDER BY c.create_time,c.id) AS rank,
		 count(*) OVER(PARTITION BY c.parent_id) AS reply_count FROM tb_comment c
		 JOIN tb_comment root ON root.id=c.parent_id AND root.type=c.type AND root.topic_id IS c.topic_id
		 WHERE c.is_review=1 AND c.is_delete=0 AND c.parent_id IN (` + marks + `))
		 SELECT ` + publicColumns + `,ranked.reply_count FROM ranked JOIN tb_comment c ON c.id=ranked.id
		 JOIN tb_user_info u ON u.id=c.user_id LEFT JOIN tb_user_info r ON r.id=c.reply_user_id
		 WHERE ranked.rank<=3 ORDER BY c.parent_id,ranked.rank`
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var total int64
			row, err := s.scanPublic(rows, true, &total)
			if err != nil {
				rows.Close()
				return nil, err
			}
			root := byID[row["parentId"].(int64)]
			root["replyCount"] = total
			root["replyDTOList"] = append(root["replyDTOList"].([]platform.Object), row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return page, tx.Commit()
}

// 用途：承接原 POST /comments 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveComment(ctx context.Context, in platform.Input) (any, error) {
	if !in.Actor.LoggedIn || in.Actor.UserInfoID <= 0 {
		return nil, failure("请先登录")
	}
	fields, err := objectInput(in.Body)
	if err != nil {
		return nil, err
	}
	kindValue, err := integer(fields["type"], true)
	if err != nil || *kindValue < 1 || *kindValue > 3 {
		return nil, invalid("评论类型不正确")
	}
	kind := int(*kindValue)
	topic, err := positiveID(fields["topicId"], kind != 2)
	if err != nil {
		return nil, err
	}
	parent, err := positiveID(fields["parentId"], false)
	if err != nil {
		return nil, err
	}
	reply, err := positiveID(fields["replyUserId"], false)
	if err != nil {
		return nil, err
	}
	if (parent == nil) != (reply == nil) {
		return nil, invalid("回复必须指定根评论和回复用户")
	}
	content, err := contentInput(fields["commentContent"], 20000)
	if err != nil {
		return nil, err
	}
	website, err := s.site.Config(ctx)
	if err != nil {
		return nil, err
	}
	approved := 1
	if configFlag(website, "isCommentReview") {
		approved = 0
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var disabled int
	if err = tx.QueryRowContext(ctx, "SELECT is_disable FROM tb_user_info WHERE id=?", in.Actor.UserInfoID).Scan(&disabled); err != nil {
		return nil, missingRow(err, "用户不存在")
	}
	if disabled != 0 {
		return nil, failure("用户已禁用")
	}
	if _, err = topicOwner(ctx, tx, kind, topic); err != nil {
		return nil, err
	}
	if parent != nil {
		root, err := visibleRoot(ctx, tx, *parent)
		if err != nil {
			return nil, err
		}
		if root.kind != kind || !sameTopic(root.topic, topic) {
			return nil, invalid("回复必须属于相同主题")
		}
		if *reply != root.userID {
			var valid bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tb_comment WHERE parent_id=? AND user_id=? AND type=? AND topic_id IS ? AND is_review=1 AND is_delete=0)`, *parent, *reply, kind, topic).Scan(&valid)
			if err != nil {
				return nil, err
			}
			if !valid {
				return nil, invalid("回复用户不属于当前评论")
			}
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO tb_comment(user_id,topic_id,comment_content,reply_user_id,parent_id,type,is_review,create_time) VALUES(?,?,?,?,?,?,?,?)`, in.Actor.UserInfoID, topic, content, reply, parent, kind, approved, s.stamp())
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	var message *mail.Message
	if configFlag(website, "isEmailNotice") {
		message, err = s.prepareNotification(ctx, tx, commentState{id: id, userID: in.Actor.UserInfoID, kind: kind, review: approved, topic: nullableID(topic), reply: nullableID(reply)})
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	notification := s.sendNotification(ctx, message)
	text := "操作成功"
	if status := notification["status"]; status == "failed" || status == "unknown" {
		text = "评论已保存，通知未提交成功"
	}
	return platform.Result{Flag: true, Code: platform.CodeOK, Message: text, Data: platform.Object{"commentId": id, "notification": notification}}, nil
}

// 用途：承接原 GET /comments/{commentId}/replies 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListRepliesByCommentId(ctx context.Context, in platform.Input) (any, error) {
	id, err := pathID(in, "commentId")
	if err != nil {
		return nil, err
	}
	current, size, err := pagination(in.Query)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	root, err := visibleRoot(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if current-1 > math.MaxInt64/size {
		return []platform.Object{}, tx.Commit()
	}
	records, err := s.readPublicComments(ctx, tx, "c.parent_id=? AND c.type=? AND c.topic_id IS ? AND c.is_review=1 AND c.is_delete=0 ORDER BY c.id LIMIT ? OFFSET ?", []any{id, root.kind, intOrNil(root.topic), size, (current - 1) * size}, true)
	if err != nil {
		return nil, err
	}
	return records, tx.Commit()
}

// 用途：承接原 POST /comments/{commentId}/like 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveCommentLike(ctx context.Context, in platform.Input) (any, error) {
	if !in.Actor.LoggedIn || in.Actor.UserInfoID <= 0 {
		return nil, failure("请先登录")
	}
	id, err := pathID(in, "commentId")
	if err != nil {
		return nil, err
	}
	_, _, err = s.engagement.Toggle(ctx, in.Actor.UserInfoID, 2, id)
	var business *platform.Error
	if errors.As(err, &business) && business.Code == platform.CodeMissing {
		return nil, errors.Join(failure(business.Message), err)
	}
	return nil, err
}

// 用途：承接原 PUT /admin/comments/review 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateCommentsReview(ctx context.Context, in platform.Input) (any, error) {
	return s.updateReview(ctx, in, true)
}

// 用途：承接原 DELETE /admin/comments 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeleteComments(ctx context.Context, in platform.Input) (any, error) {
	return s.deleteRecords(ctx, in, true)
}

// 用途：承接原 GET /admin/comments 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListCommentBackDTO(ctx context.Context, in platform.Input) (any, error) {
	return s.adminPage(ctx, in, true)
}

// 用途：承接原 POST /messages 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveMessage(ctx context.Context, in platform.Input) (any, error) {
	fields, err := objectInput(in.Body)
	if err != nil {
		return nil, err
	}
	nickname, err := textInput(fields["nickname"])
	if err != nil {
		return nil, err
	}
	avatar, err := textInput(fields["avatar"])
	if err != nil || !safeURL(avatar, true) {
		return nil, invalid("头像地址不正确")
	}
	duration, err := integer(fields["time"], true)
	if err != nil || *duration < math.MinInt32 || *duration > math.MaxInt32 {
		return nil, invalid("弹幕时间不正确")
	}
	content, err := contentInput(fields["messageContent"], 255)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(in.IP)
	if ip == nil {
		return nil, invalid("请求来源不正确")
	}
	website, err := s.site.Config(ctx)
	if err != nil {
		return nil, err
	}
	approved := 1
	if configFlag(website, "isMessageReview") {
		approved = 0
	}
	source := s.analytics.LookupIP(ctx, ip.String())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// The existing message fact owns the per-IP POST cooldown. Checking it in
	// the same IMMEDIATE transaction as insertion prevents concurrent bypasses
	// and preserves the five-second boundary through a process restart.
	var last string
	err = tx.QueryRowContext(ctx, "SELECT create_time FROM tb_message WHERE ip_address=? ORDER BY id DESC LIMIT 1", ip.String()).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	now := s.now()
	if err == nil {
		at, parseErr := s.parseTime(last)
		if parseErr != nil {
			return nil, parseErr
		}
		if now.Sub(at) < 5*time.Second {
			return nil, failure("操作过于频繁，请稍后重试")
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO tb_message(nickname,avatar,message_content,ip_address,ip_source,time,is_review,create_time) VALUES(?,?,?,?,?,?,?,?)", nickname, avatar, content, ip.String(), source, *duration, approved, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 GET /messages 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListMessages(ctx context.Context, in platform.Input) (any, error) {
	return s.readMessages(ctx, s.db, "is_review=1 ORDER BY id", nil, false)
}

// 用途：承接原 GET /admin/messages 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListMessageBackDTO(ctx context.Context, in platform.Input) (any, error) {
	return s.adminPage(ctx, in, false)
}

// 用途：承接原 PUT /admin/messages/review 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateMessagesReview(ctx context.Context, in platform.Input) (any, error) {
	return s.updateReview(ctx, in, false)
}

// 用途：承接原 DELETE /admin/messages 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeleteMessages(ctx context.Context, in platform.Input) (any, error) {
	return s.deleteRecords(ctx, in, false)
}

type reader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type commentState struct {
	id, userID            int64
	kind, review, deleted int
	topic, parent, reply  sql.NullInt64
}

func invalid(message string) error {
	return &platform.Error{Code: platform.CodeInvalid, Message: message}
}
func failure(message string) error { return &platform.Error{Code: platform.CodeFail, Message: message} }
func missingRow(err error, message string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return errors.Join(failure(message), err)
	}
	return err
}

func nullableID(id *int64) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *id, Valid: true}
}
func sameTopic(stored sql.NullInt64, topic *int64) bool {
	return (!stored.Valid && topic == nil) || (stored.Valid && topic != nil && stored.Int64 == *topic)
}

func objectInput(body json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if !utf8.Valid(body) || json.Unmarshal(body, &fields) != nil || fields == nil {
		return nil, invalid("请求字段不正确")
	}
	return fields, nil
}

func integer(raw json.RawMessage, required bool) (*int64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		if required {
			return nil, invalid("缺少必填数字字段")
		}
		return nil, nil
	}
	var n int64
	if raw[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return nil, invalid("数字字段不正确")
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, invalid("数字字段不正确")
		}
		n = parsed
	} else if json.Unmarshal(raw, &n) != nil {
		return nil, invalid("数字字段不正确")
	}
	return &n, nil
}

func positiveID(raw json.RawMessage, required bool) (*int64, error) {
	id, err := integer(raw, required)
	if err != nil {
		return nil, err
	}
	if id != nil && *id <= 0 {
		return nil, invalid("ID必须为正整数")
	}
	return id, nil
}
func textInput(raw json.RawMessage) (string, error) {
	var value string
	if json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || strings.TrimSpace(value) == "" {
		return "", invalid("文本字段不能为空")
	}
	return strings.TrimSpace(value), nil
}
func contentInput(raw json.RawMessage, limit int) (string, error) {
	value, err := textInput(raw)
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(value) > limit {
		return "", invalid(fmt.Sprintf("内容不能超过%d个字符", limit))
	}
	return SanitizeHTML(value)
}
func configFlag(config platform.Object, key string) bool { return config[key] == float64(1) }
func (s *Service) stamp() string                         { return s.now().UTC().Format(time.RFC3339Nano) }
func (s *Service) parseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if at, err := time.ParseInLocation(layout, value, s.cfg.TimeZone); err == nil {
			return at, nil
		}
	}
	return time.Time{}, errors.New("invalid stored community creation time")
}
func (s *Service) localTime(value string) (string, error) {
	at, err := s.parseTime(value)
	if err != nil {
		return "", err
	}
	return at.In(s.cfg.TimeZone).Format("2006-01-02T15:04:05"), nil
}

func queryInt(query url.Values, key string, required bool) (*int64, error) {
	values, ok := query[key]
	if !ok {
		if required {
			return nil, invalid("缺少查询参数")
		}
		return nil, nil
	}
	if len(values) != 1 {
		return nil, invalid("查询参数不正确")
	}
	n, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil {
		return nil, invalid("查询参数不正确")
	}
	return &n, nil
}
func commentQuery(query url.Values) (int, *int64, error) {
	kind, err := queryInt(query, "type", true)
	if err != nil || *kind < 1 || *kind > 3 {
		return 0, nil, invalid("评论类型不正确")
	}
	topic, err := queryInt(query, "topicId", *kind != 2)
	if err != nil {
		return 0, nil, err
	}
	if topic != nil && *topic <= 0 {
		return 0, nil, invalid("主题ID不正确")
	}
	return int(*kind), topic, nil
}
func pagination(query url.Values) (int64, int64, error) {
	current, size := int64(1), int64(10)
	for _, key := range []string{"current", "size"} {
		n, err := queryInt(query, key, false)
		if err != nil {
			return 0, 0, err
		}
		if n == nil {
			continue
		}
		if *n < 1 || (key == "size" && *n > 100) {
			return 0, 0, invalid("分页参数不正确")
		}
		if key == "current" {
			current = *n
		} else {
			size = *n
		}
	}
	return current, size, nil
}
func pathID(in platform.Input, key string) (int64, error) {
	id, err := strconv.ParseInt(in.Params[key], 10, 64)
	if err != nil || id <= 0 {
		return 0, invalid("ID不正确")
	}
	return id, nil
}
func idParameters(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}
func decodeIDs(raw json.RawMessage) ([]int64, error) {
	var members []json.RawMessage
	if json.Unmarshal(raw, &members) != nil || len(members) == 0 {
		return nil, invalid("请选择记录")
	}
	ids := make([]int64, 0, len(members))
	seen := make(map[int64]bool)
	for _, raw := range members {
		id, err := positiveID(raw, true)
		if err != nil {
			return nil, err
		}
		if !seen[*id] {
			ids = append(ids, *id)
			seen[*id] = true
		}
	}
	return ids, nil
}

func topicOwner(ctx context.Context, db reader, kind int, topic *int64) (int64, error) {
	if kind == 2 {
		return 1, nil
	}
	if topic == nil {
		return 0, invalid("缺少评论主题")
	}
	query := "SELECT user_id FROM tb_article WHERE id=? AND status=1 AND is_delete=0"
	if kind == 3 {
		query = "SELECT user_id FROM tb_talk WHERE id=? AND status=1"
	}
	var owner int64
	err := db.QueryRowContext(ctx, query, *topic).Scan(&owner)
	return owner, missingRow(err, "评论主题不存在或不可见")
}

func visibleRoot(ctx context.Context, db reader, id int64) (commentState, error) {
	var root commentState
	err := db.QueryRowContext(ctx, "SELECT id,user_id,type,topic_id,parent_id,reply_user_id,is_review,is_delete FROM tb_comment WHERE id=?", id).Scan(&root.id, &root.userID, &root.kind, &root.topic, &root.parent, &root.reply, &root.review, &root.deleted)
	if err != nil {
		return root, missingRow(err, "评论不存在")
	}
	if root.parent.Valid || root.review != 1 || root.deleted != 0 {
		return root, failure("根评论不存在或不可见")
	}
	var topic *int64
	if root.topic.Valid {
		topic = &root.topic.Int64
	}
	_, err = topicOwner(ctx, db, root.kind, topic)
	return root, err
}

const publicColumns = `c.id,c.user_id,u.nickname,u.avatar,u.web_site,c.comment_content,c.create_time,
 (SELECT count(*) FROM tb_like l WHERE l.target_type=2 AND l.target_id=c.id),c.parent_id,c.reply_user_id,r.nickname,r.web_site`

func (s *Service) scanPublic(rows *sql.Rows, reply bool, extra ...any) (platform.Object, error) {
	var id, userID, likes int64
	var nickname, avatar, content, created string
	var website, replyNickname, replyWebsite sql.NullString
	var parent, replyID sql.NullInt64
	targets := []any{&id, &userID, &nickname, &avatar, &website, &content, &created, &likes, &parent, &replyID, &replyNickname, &replyWebsite}
	targets = append(targets, extra...)
	if err := rows.Scan(targets...); err != nil {
		return nil, err
	}
	local, err := s.localTime(created)
	if err != nil {
		return nil, err
	}
	row := platform.Object{"id": id, "userId": userID, "nickname": nickname, "avatar": avatar, "webSite": stringOrNil(website), "commentContent": content, "createTime": local, "likeCount": likes}
	if reply {
		row["parentId"] = intOrNil(parent)
		row["replyUserId"] = intOrNil(replyID)
		row["replyNickname"] = stringOrNil(replyNickname)
		row["replyWebSite"] = stringOrNil(replyWebsite)
	}
	return row, nil
}

func intOrNil(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
}
func stringOrNil(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func (s *Service) readPublicComments(ctx context.Context, db reader, where string, args []any, reply bool) ([]platform.Object, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+publicColumns+" FROM tb_comment c JOIN tb_user_info u ON u.id=c.user_id LEFT JOIN tb_user_info r ON r.id=c.reply_user_id WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []platform.Object{}
	for rows.Next() {
		row, err := s.scanPublic(rows, reply)
		if err != nil {
			return nil, err
		}
		records = append(records, row)
	}
	return records, rows.Err()
}

func (s *Service) prepareNotification(ctx context.Context, db reader, state commentState) (*mail.Message, error) {
	target := int64(1)
	if state.review == 1 {
		if state.reply.Valid {
			target = state.reply.Int64
		} else if state.kind == 1 || state.kind == 3 {
			query := "SELECT user_id FROM tb_article WHERE id=?"
			if state.kind == 3 {
				query = "SELECT user_id FROM tb_talk WHERE id=?"
			}
			if err := db.QueryRowContext(ctx, query, intOrNil(state.topic)).Scan(&target); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, nil
				}
				return nil, err
			}
		}
	}
	var email sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT email FROM tb_user_info WHERE id=?", target).Scan(&email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !email.Valid || strings.TrimSpace(email.String) == "" {
		return nil, nil
	}
	message := &mail.Message{Purpose: "comment", BusinessID: &state.id, Recipient: strings.TrimSpace(email.String), Subject: "评论提醒"}
	if state.review == 0 {
		message.Subject = "审核提醒"
		message.Text = "您收到了一条新的回复，请前往后台管理页面审核"
	} else {
		path := "/links/"
		if state.kind == 1 {
			path = "/articles/"
		} else if state.kind == 3 {
			path = "/talks/"
		}
		if state.topic.Valid {
			path += strconv.FormatInt(state.topic.Int64, 10)
		}
		message.Text = "您收到了一条新的回复，请前往" + strings.TrimRight(s.cfg.FrontendURL, "/") + path + "\n页面查看"
	}
	return message, nil
}

func (s *Service) sendNotification(ctx context.Context, message *mail.Message) platform.Object {
	if message == nil {
		return platform.Object{"status": "skipped", "deliveryId": nil}
	}
	// Mail exposes a durable submission status even on error. Propagate that
	// status without turning an already committed comment into an HTTP failure.
	outcome, _ := s.mail.Send(ctx, *message)
	return platform.Object{"status": outcome.Status, "deliveryId": outcome.DeliveryID}
}

func (s *Service) updateReview(ctx context.Context, in platform.Input, comments bool) (any, error) {
	fields, err := objectInput(in.Body)
	if err != nil {
		return nil, err
	}
	ids, err := decodeIDs(fields["idList"])
	if err != nil {
		return nil, err
	}
	flag, err := integer(fields["isReview"], true)
	if err != nil || (*flag != 0 && *flag != 1) {
		return nil, invalid("审核状态必须为0或1")
	}
	notice := false
	if comments {
		website, err := s.site.Config(ctx)
		if err != nil {
			return nil, err
		}
		notice = configFlag(website, "isEmailNotice")
	}
	table := "tb_message"
	if comments {
		table = "tb_comment"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	messages := []*mail.Message{}
	for start := 0; start < len(ids); start += 500 {
		batch := ids[start:min(start+500, len(ids))]
		marks, args := idParameters(batch)
		var found int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id IN ("+marks+")", args...).Scan(&found); err != nil {
			return nil, err
		}
		if found != len(batch) {
			return nil, failure("审核记录不存在")
		}
		if comments && notice && *flag == 1 {
			rows, err := tx.QueryContext(ctx, "SELECT id,user_id,type,topic_id,reply_user_id FROM tb_comment WHERE is_review=0 AND is_delete=0 AND id IN ("+marks+")", args...)
			if err != nil {
				return nil, err
			}
			states := []commentState{}
			for rows.Next() {
				state := commentState{review: 1}
				if err := rows.Scan(&state.id, &state.userID, &state.kind, &state.topic, &state.reply); err != nil {
					rows.Close()
					return nil, err
				}
				states = append(states, state)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return nil, err
			}
			rows.Close()
			for _, state := range states {
				message, err := s.prepareNotification(ctx, tx, state)
				if err != nil {
					return nil, err
				}
				if message != nil {
					messages = append(messages, message)
				}
			}
		}
		updateArgs := append([]any{*flag, s.stamp()}, args...)
		if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET is_review=?,update_time=? WHERE id IN ("+marks+")", updateArgs...); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	failed := false
	for _, message := range messages {
		outcome := s.sendNotification(ctx, message)
		if outcome["status"] != "accepted" {
			failed = true
		}
	}
	if failed {
		return platform.Result{Flag: true, Code: platform.CodeOK, Message: "审核已保存，通知未提交成功"}, nil
	}
	return nil, nil
}

func (s *Service) deleteRecords(ctx context.Context, in platform.Input, comments bool) (any, error) {
	ids, err := decodeIDs(in.Body)
	if err != nil {
		return nil, err
	}
	table := "tb_message"
	if comments {
		table = "tb_comment"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Validate the full selection before cascades can remove an ID named in a
	// later batch. All state and like cleanup share this transaction.
	for start := 0; start < len(ids); start += 500 {
		batch := ids[start:min(start+500, len(ids))]
		marks, args := idParameters(batch)
		var found int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id IN ("+marks+")", args...).Scan(&found); err != nil {
			return nil, err
		}
		if found != len(batch) {
			return nil, failure("删除记录不存在")
		}
	}
	for start := 0; start < len(ids); start += 500 {
		marks, args := idParameters(ids[start:min(start+500, len(ids))])
		if comments {
			rows, err := tx.QueryContext(ctx, `WITH RECURSIVE removed(id) AS (SELECT id FROM tb_comment WHERE id IN (`+marks+`) UNION SELECT c.id FROM tb_comment c JOIN removed r ON c.parent_id=r.id) SELECT id FROM removed`, args...)
			if err != nil {
				return nil, err
			}
			removed := []int64{}
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return nil, err
				}
				removed = append(removed, id)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return nil, err
			}
			rows.Close()
			if err := s.engagement.DeleteTargetsTx(ctx, tx, 2, removed); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id IN ("+marks+")", args...); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}

func (s *Service) adminPage(ctx context.Context, in platform.Input, comments bool) (any, error) {
	current, size, err := pagination(in.Query)
	if err != nil {
		return nil, err
	}
	where := "1=1"
	args := []any{}
	alias := ""
	if comments {
		alias = "c."
	}
	for _, key := range []string{"isReview", "type"} {
		if key == "type" && !comments {
			continue
		}
		value, err := queryInt(in.Query, key, false)
		if err != nil {
			return nil, err
		}
		if value != nil {
			column := "is_review"
			if key == "type" {
				column = "type"
				if *value < 1 || *value > 3 {
					return nil, invalid("评论类型不正确")
				}
			} else if *value != 0 && *value != 1 {
				return nil, invalid("审核状态不正确")
			}
			where += " AND " + alias + column + "=?"
			args = append(args, *value)
		}
	}
	pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.TrimSpace(in.Query.Get("keywords"))) + "%"
	nickname := "nickname"
	table := "tb_message"
	if comments {
		nickname = "u.nickname"
		table = "tb_comment c LEFT JOIN tb_user_info u ON u.id=c.user_id LEFT JOIN tb_user_info r ON r.id=c.reply_user_id LEFT JOIN tb_article a ON c.type=1 AND a.id=c.topic_id"
	}
	where += " AND " + nickname + " LIKE ? ESCAPE '\\'"
	args = append(args, pattern)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	page := platform.Page{RecordList: []platform.Object{}}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE "+where, args...).Scan(&page.Count); err != nil {
		return nil, err
	}
	if page.Count == 0 || current-1 > (page.Count-1)/size {
		return page, tx.Commit()
	}
	args = append(args, size, (current-1)*size)
	if !comments {
		page.RecordList, err = s.readMessages(ctx, tx, where+" ORDER BY id DESC LIMIT ? OFFSET ?", args, true)
		if err != nil {
			return nil, err
		}
		return page, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, "SELECT c.id,u.avatar,u.nickname,r.nickname,a.article_title,c.comment_content,c.type,c.is_review,c.create_time FROM "+table+" WHERE "+where+" ORDER BY c.id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var kind, review int
		var avatar, nickname, reply, title sql.NullString
		var content, created string
		if err := rows.Scan(&id, &avatar, &nickname, &reply, &title, &content, &kind, &review, &created); err != nil {
			rows.Close()
			return nil, err
		}
		local, err := s.localTime(created)
		if err != nil {
			rows.Close()
			return nil, err
		}
		page.RecordList = append(page.RecordList, platform.Object{"id": id, "avatar": stringOrNil(avatar), "nickname": stringOrNil(nickname), "replyNickname": stringOrNil(reply), "articleTitle": stringOrNil(title), "commentContent": content, "type": kind, "isReview": review, "createTime": local})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return page, tx.Commit()
}

func (s *Service) readMessages(ctx context.Context, db reader, where string, args []any, admin bool) ([]platform.Object, error) {
	columns := "id,nickname,avatar,message_content,time"
	if admin {
		columns = "id,nickname,avatar,message_content,ip_address,ip_source,is_review,create_time"
	}
	rows, err := db.QueryContext(ctx, "SELECT "+columns+" FROM tb_message WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []platform.Object{}
	for rows.Next() {
		var id int64
		var nickname, avatar, content, ip, source, created string
		var review int
		var duration sql.NullInt64
		targets := []any{&id, &nickname, &avatar, &content}
		if admin {
			targets = append(targets, &ip, &source, &review, &created)
		} else {
			targets = append(targets, &duration)
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		row := platform.Object{"id": id, "nickname": nickname, "avatar": avatar, "messageContent": content}
		if admin {
			local, err := s.localTime(created)
			if err != nil {
				return nil, err
			}
			row["ipAddress"] = ip
			row["ipSource"] = source
			row["isReview"] = review
			row["createTime"] = local
		} else {
			row["time"] = intOrNil(duration)
		}
		records = append(records, row)
	}
	return records, rows.Err()
}

// 用途：将本模块 schema.sql 交给唯一迁移器，业务逻辑不自动建表。
//
//go:embed schema.sql
var Schema string

// Public protocol projections use platform.Object; no database row is serialized directly.
// Integer/Long: JSON number; Date: Unix milliseconds; LocalDateTime: yyyy-MM-ddTHH:mm:ss.
// Optional fields retain null; lists/sets are arrays. UserDetailDTO never includes password.
// CommentDTO: id:Integer, userId:Integer, nickname:String, avatar:String, webSite:String, commentContent:String, likeCount:Integer, createTime:LocalDateTime, replyCount:Integer, replyDTOList:ReplyDTO[]
// ReplyDTO: id:Integer, parentId:Integer, userId:Integer, nickname:String, avatar:String, webSite:String, replyUserId:Integer, replyNickname:String, replyWebSite:String, commentContent:String, likeCount:Integer, createTime:LocalDateTime
// CommentBackDTO: id:Integer, avatar:String, nickname:String, replyNickname:String, articleTitle:String, commentContent:String, type:Integer, isReview:Integer, createTime:LocalDateTime
// MessageDTO: id:Integer, nickname:String, avatar:String, messageContent:String, time:Integer
// MessageBackDTO: id:Integer, ipAddress:String, ipSource:String, nickname:String, avatar:String, messageContent:String, isReview:Integer, createTime:LocalDateTime
// CommentVO: replyUserId:Integer, topicId:Integer, commentContent:String, parentId:Integer, type:Integer
// CommentQueryVO: topicId:Integer, parentId:Integer, type:Integer
// MessageVO: nickname:String, avatar:String, messageContent:String, time:Integer
