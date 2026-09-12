package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/gin-gonic/gin"

	"blog-go/internal/app"
	"blog-go/internal/platform"
)

// 用途：加载配置、接收中断信号并运行唯一 Go 后端。
func main() {
	gin.SetMode(gin.ReleaseMode)
	cfg, err := platform.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	instance, err := app.New(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法初始化本地博客：", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "本地管理员初始凭据文件：%s\n", filepath.Join(cfg.DataDir, "initial-admin.txt"))
	if err := instance.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "本地博客运行失败：", err)
		os.Exit(1)
	}
}

// P-123 CLI CLI scripts/start.sh
// 约束变化：构建并启动本目录二进制，保存所属PID；冲突拒绝，退出码0仅表示成功启动。 具体字段与状态解释见MODULE-014已确认执行边界。

// P-124 CLI CLI scripts/stop.sh
// 约束变化：只停止核对为本项目的PID，优雅退出，保留DB/文件。 具体字段与状态解释见MODULE-014已确认执行边界。

// P-125 CLI CLI python3 scripts/prepare-frontends.py
// 约束变化：读取原前端到受控副本；锁源转换不改版本/integrity，失败非0；不覆盖原前端。 具体字段与状态解释见MODULE-014已确认执行边界。

// P-126 CLI CLI scripts/verify.sh
// 约束变化：执行Go单测/竞态/构建与独立本地集成套件；真实前端验收另有记录，任何失败退出非0。 具体字段与状态解释见MODULE-014已确认执行边界。
