package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"blog-go/internal/analytics"
	"blog-go/internal/article"
	"blog-go/internal/bootstrap"
	"blog-go/internal/catalog"
	"blog-go/internal/chat"
	"blog-go/internal/community"
	"blog-go/internal/engagement"
	"blog-go/internal/files"
	"blog-go/internal/identity"
	"blog-go/internal/mail"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
	"blog-go/internal/social"
)

// 用途：拥有 HTTP/聊天/数据库的统一启动和停止生命周期。
type App struct {
	Config    platform.Config
	DB        *sql.DB
	Engine    *gin.Engine
	HTTP      *http.Server
	Chat      *chat.Service
	reconcile func(context.Context, time.Time) error
}

// 用途：按本评审依赖图构造模块、汇集 schema，构建 Gin Engine 并设置为 http.Server.Handler。
func New(ctx context.Context, cfg platform.Config) (instance *App, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.TimeZone == nil {
		return nil, errors.New("application timezone is required")
	}
	db, err := platform.OpenSQLite(cfg)
	if err != nil {
		return nil, err
	}
	ready := false
	var ownedChat *chat.Service
	defer func() {
		if !ready {
			if ownedChat != nil {
				closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				err = errors.Join(err, ownedChat.Close(closeContext))
			}
			err = errors.Join(err, db.Close())
		}
	}()
	schemas := []string{identity.Schema, catalog.Schema, article.Schema, community.Schema, media.Schema,
		site.Schema, analytics.Schema, mail.Schema, chat.Schema, engagement.Schema}
	if err := platform.Migrate(ctx, db, schemas, func(ctx context.Context, tx *sql.Tx) error {
		return bootstrap.Seed(ctx, tx, cfg)
	}); err != nil {
		return nil, err
	}
	now := func() time.Time { return time.Now().In(cfg.TimeZone) }
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	siteService := site.New(db, cfg, now)
	analyticsService := analytics.New(db, cfg, client, now)
	filesService := files.New(db, cfg, client, now)
	mailService := mail.New(db, cfg, &mail.SMTP{Config: cfg}, now)
	socialService := social.New(db, cfg, client, now)
	engagementService := engagement.New(db, cfg, now)
	catalogService := catalog.New(db, cfg, now)
	identityService := identity.New(db, cfg, filesService, mailService, socialService,
		siteService, analyticsService, engagementService, now)
	articleService := article.New(db, cfg, catalogService, siteService, filesService, engagementService, now)
	communityService := community.New(db, cfg, siteService, analyticsService, mailService, engagementService, now)
	mediaService := media.New(db, cfg, engagementService, now)
	chatService := chat.New(db, cfg, filesService, analyticsService, identityService.ActorForVisitor, now)
	ownedChat = chatService
	var routes []platform.Route
	for _, group := range [][]platform.Route{
		identityService.Routes(), articleService.Routes(), catalogService.Routes(), communityService.Routes(),
		mediaService.Routes(), siteService.Routes(), analyticsService.Routes(), filesService.Routes(), chatService.Routes(),
	} {
		routes = append(routes, group...)
	}
	routes = append(routes,
		platform.Route{Spec: platform.RouteSpec{Method: "GET", Path: "/healthz", Operation: "Health", Policy: "public"}, Raw: platform.Health(db)},
		platform.Route{Spec: platform.RouteSpec{Method: "GET", Path: "/websocket", Operation: "ServeWebSocket", Policy: "public"}, Raw: http.HandlerFunc(chatService.ServeWebSocket)},
		platform.Route{Spec: platform.RouteSpec{Method: "GET", Path: "/files/{path...}", Operation: "ServeFile", Policy: "public"}, Raw: http.HandlerFunc(filesService.ServeFile)},
		platform.Route{Spec: platform.RouteSpec{Method: "HEAD", Path: "/files/{path...}", Operation: "ServeFile", Policy: "public"}, Raw: http.HandlerFunc(filesService.ServeFile)},
	)
	engine, err := platform.NewRouter(routes, platform.Callbacks{
		Resolve: identityService.Resolve, Authorize: identityService.Authorize, Audit: analyticsService.Audit,
	}, cfg)
	if err != nil {
		return nil, err
	}
	if err := analyticsService.ReconcileDays(ctx, now()); err != nil {
		return nil, fmt.Errorf("reconcile visits on startup: %w", err)
	}
	instance = &App{
		Config: cfg, DB: db, Engine: engine, Chat: chatService, reconcile: analyticsService.ReconcileDays,
		HTTP: &http.Server{
			Addr: cfg.Addr, Handler: engine, ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
			MaxHeaderBytes: 1 << 20,
		},
	}
	ready = true
	return instance, nil
}

// 用途：监听本机并响应取消信号，优雅清理拥有的资源。
func (a *App) Run(ctx context.Context) (err error) {
	var serverFinished chan struct{}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if a.HTTP != nil {
			if shutdownErr := a.HTTP.Shutdown(shutdownContext); shutdownErr != nil {
				_ = a.HTTP.Close()
				err = errors.Join(err, shutdownErr)
			}
		}
		if a.Chat != nil {
			err = errors.Join(err, a.Chat.Close(shutdownContext))
		}
		if a.DB != nil {
			err = errors.Join(err, a.DB.Close())
		}
		if serverFinished != nil {
			<-serverFinished
		}
	}()
	if ctx.Err() != nil {
		return nil
	}
	if a.HTTP == nil {
		return errors.New("application HTTP server is required")
	}
	listener, err := net.Listen("tcp", a.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen for HTTP: %w", err)
	}
	serverResult := make(chan error, 1)
	serverFinished = make(chan struct{})
	go func() {
		defer close(serverFinished)
		serverResult <- a.HTTP.Serve(listener)
	}()
	location := a.Config.TimeZone
	if location == nil {
		location = time.Local
	}
	untilMidnight := func() time.Duration {
		now := time.Now().In(location)
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, location)
		return next.Sub(now)
	}
	timer := time.NewTimer(untilMidnight())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-serverResult:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return fmt.Errorf("serve HTTP: %w", err)
		case <-timer.C:
			if a.reconcile != nil {
				if err := a.reconcile(ctx, time.Now().In(location)); err != nil {
					log.Print("daily visit reconciliation failed")
				}
			}
			timer.Reset(untilMidnight())
		}
	}
}
