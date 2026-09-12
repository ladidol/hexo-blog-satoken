package engagement

import (
	"database/sql"
	_ "embed"
	"time"

	"blog-go/internal/platform"
)

// 用途：拥有点赞与文章浏览事实的状态、数据访问及下列显式依赖。
type Service struct {
	db  *sql.DB
	cfg platform.Config
	now func() time.Time
}

// 用途：构造点赞与文章浏览事实，由 app 唯一装配依赖。
func New(db *sql.DB, cfg platform.Config, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.TimeZone == nil {
		cfg.TimeZone = time.UTC
	}
	return &Service{db: db, cfg: cfg, now: now}
}

func (s *Service) timestamp() string {
	return s.now().In(s.cfg.TimeZone).Format("2006-01-02T15:04:05")
}

// 用途：将本模块 schema.sql 交给唯一迁移器，业务逻辑不自动建表。
//
//go:embed schema.sql
var Schema string
