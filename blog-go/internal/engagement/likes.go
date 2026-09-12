package engagement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"blog-go/internal/platform"
)

// 用途：单事务切换指定公开/审核目标的用户点赞事实。
func (s *Service) Toggle(ctx context.Context, userInfoID int64, targetType int, targetID int64) (bool, int64, error) {
	if userInfoID <= 0 {
		return false, 0, &platform.Error{Code: platform.CodeInvalid, Message: "用户资料 ID 无效"}
	}
	if err := validateTarget(targetType, targetID); err != nil {
		return false, 0, err
	}
	// OpenSQLite uses BEGIN IMMEDIATE: visibility, toggle and returned count
	// share a single serialized write transaction, including across DB handles.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, fmt.Errorf("begin like transaction: %w", err)
	}
	defer tx.Rollback()
	var disabled int
	if err := tx.QueryRowContext(ctx, "SELECT is_disable FROM tb_user_info WHERE id=?", userInfoID).Scan(&disabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, 0, &platform.Error{Code: platform.CodeMissing, Message: "用户不存在"}
		}
		return false, 0, fmt.Errorf("read like owner: %w", err)
	}
	if disabled != 0 {
		return false, 0, &platform.Error{Code: platform.CodeFail, Message: "用户已禁用"}
	}
	if err := visibleTargetTx(ctx, tx, targetType, targetID); err != nil {
		return false, 0, err
	}
	var existing bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tb_like
		WHERE user_info_id=? AND target_type=? AND target_id=?)`, userInfoID, targetType, targetID).Scan(&existing); err != nil {
		return false, 0, fmt.Errorf("read like fact: %w", err)
	}
	if existing {
		_, err = tx.ExecContext(ctx, "DELETE FROM tb_like WHERE user_info_id=? AND target_type=? AND target_id=?", userInfoID, targetType, targetID)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO tb_like(user_info_id,target_type,target_id,create_time) VALUES(?,?,?,?)", userInfoID, targetType, targetID, s.timestamp())
	}
	if err != nil {
		return false, 0, fmt.Errorf("toggle like fact: %w", err)
	}
	var count int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM tb_like WHERE target_type=? AND target_id=?", targetType, targetID).Scan(&count); err != nil {
		return false, 0, fmt.Errorf("count toggled likes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, 0, fmt.Errorf("commit like transaction: %w", err)
	}
	return !existing, count, nil
}

// 用途：读取目标的真实点赞数量供原详情/列表展示。
func (s *Service) Count(ctx context.Context, targetType int, targetID int64) (int64, error) {
	if err := validateTarget(targetType, targetID); err != nil {
		return 0, err
	}
	var count int64
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM tb_like WHERE target_type=? AND target_id=?", targetType, targetID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count likes: %w", err)
	}
	return count, nil
}

// 用途：返回当前用户三类点赞数字集合供两端登录资料消费。
func (s *Service) UserSets(ctx context.Context, userInfoID int64) (platform.Object, error) {
	if userInfoID <= 0 {
		return nil, &platform.Error{Code: platform.CodeInvalid, Message: "用户资料 ID 无效"}
	}
	rows, err := s.db.QueryContext(ctx, "SELECT target_type,target_id FROM tb_like WHERE user_info_id=? ORDER BY target_type,target_id", userInfoID)
	if err != nil {
		return nil, fmt.Errorf("read user likes: %w", err)
	}
	defer rows.Close()
	sets := [3][]int64{{}, {}, {}}
	for rows.Next() {
		var typ int
		var id int64
		if err := rows.Scan(&typ, &id); err != nil {
			return nil, fmt.Errorf("scan user like: %w", err)
		}
		if err := validateTarget(typ, id); err != nil {
			return nil, fmt.Errorf("invalid stored like: %w", err)
		}
		sets[typ-1] = append(sets[typ-1], id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read user like rows: %w", err)
	}
	return platform.Object{"articleLikeSet": sets[0], "commentLikeSet": sets[1], "talkLikeSet": sets[2]}, nil
}

// 用途：业务永久删除时在同一事务清理目标的点赞事实。
func (s *Service) DeleteTargetsTx(ctx context.Context, tx *sql.Tx, targetType int, ids []int64) error {
	if tx == nil {
		return &platform.Error{Code: platform.CodeInvalid, Message: "清理点赞需要业务事务"}
	}
	if err := validateTarget(targetType, 1); err != nil {
		return err
	}
	for _, id := range ids {
		if err := validateTarget(targetType, id); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Keep statement size bounded; the caller owns commit/rollback of all batches.
	const batchSize = 500
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		args := make([]any, 1, end-start+1)
		args[0] = targetType
		for _, id := range ids[start:end] {
			args = append(args, id)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", end-start), ",")
		if _, err := tx.ExecContext(ctx, "DELETE FROM tb_like WHERE target_type=? AND target_id IN ("+placeholders+")", args...); err != nil {
			return fmt.Errorf("delete target likes: %w", err)
		}
	}
	return nil
}

func validateTarget(targetType int, targetID int64) error {
	if targetType < 1 || targetType > 3 || targetID <= 0 {
		return &platform.Error{Code: platform.CodeInvalid, Message: "互动目标无效"}
	}
	return nil
}

func visibleTargetTx(ctx context.Context, tx *sql.Tx, targetType int, targetID int64) error {
	var query string
	switch targetType {
	case 1:
		query = "SELECT 1 FROM tb_article WHERE id=? AND status=1 AND is_delete=0"
	case 2:
		query = `SELECT 1 FROM tb_comment c WHERE c.id=? AND c.is_review=1 AND c.is_delete=0
			AND (c.type=2
			OR (c.type=1 AND EXISTS(SELECT 1 FROM tb_article a WHERE a.id=c.topic_id AND a.status=1 AND a.is_delete=0))
			OR (c.type=3 AND EXISTS(SELECT 1 FROM tb_talk t WHERE t.id=c.topic_id AND t.status=1)))`
	case 3:
		query = "SELECT 1 FROM tb_talk WHERE id=? AND status=1"
	default:
		return &platform.Error{Code: platform.CodeInvalid, Message: "互动目标类型无效"}
	}
	var found int
	if err := tx.QueryRowContext(ctx, query, targetID).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &platform.Error{Code: platform.CodeMissing, Message: "互动目标不存在或不可见"}
		}
		return fmt.Errorf("read interactive target: %w", err)
	}
	return nil
}
