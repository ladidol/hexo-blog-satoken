package engagement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"blog-go/internal/platform"
)

// 用途：为可公开文章记录同浏览会话首看事实并返回累计浏览数。
func (s *Service) RecordView(ctx context.Context, articleID int64, visitorKey string) (int64, error) {
	if err := validateTarget(1, articleID); err != nil {
		return 0, err
	}
	if visitorKey == "" {
		return 0, &platform.Error{Code: platform.CodeInvalid, Message: "浏览会话不能为空"}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin article view transaction: %w", err)
	}
	defer tx.Rollback()
	if err := visibleTargetTx(ctx, tx, 1, articleID); err != nil {
		return 0, err
	}
	// The input is the opaque browser cookie, never a login identity or IP.
	hash := sha256.Sum256([]byte(visitorKey))
	if _, err := tx.ExecContext(ctx, `INSERT INTO tb_article_view(article_id,visitor_key,create_time)
		VALUES(?,?,?) ON CONFLICT(article_id,visitor_key) DO NOTHING`, articleID, hex.EncodeToString(hash[:]), s.timestamp()); err != nil {
		return 0, fmt.Errorf("record article view: %w", err)
	}
	var count int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM tb_article_view WHERE article_id=?", articleID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count article views: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit article view transaction: %w", err)
	}
	return count, nil
}
