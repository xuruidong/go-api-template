// Command server 启动 Todo API HTTP 服务。
//
// 启动流程：
//
//	读取配置 -> 构造依赖（Store、Logger）-> 组装路由 -> 监听 -> 优雅关闭
//
// 运行方式见 README.md 或 Makefile（make run）。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/go-api-template/internal/config"
	"github.com/example/go-api-template/internal/server"
	"github.com/example/go-api-template/internal/store"
)

// version 由构建时注入：
//
//	go build -ldflags "-X main.version=$(git describe --tags --always --dirty)"
//
// 未注入时为 dev，会出现在 /healthz 响应与服务日志中，便于确认线上运行版本。
var version = "dev"

func main() {
	if err := run(); err != nil {
		// 启动期错误用 stderr 输出，此时结构化的 logger 可能还没准备好。
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	// 内存存储仅用于示例；接入数据库时替换为真实的 store.Store 实现即可，
	// server 层代码不需要改动。
	st := store.NewMemory()

	srv := server.New(st, logger, cfg.AuthToken, version)
	handler, err := srv.Handler()
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	// 监听 SIGINT / SIGTERM：收到信号后停止接收新请求，等待在途请求处理完再退出。
	// 这是容器环境下滚动更新不丢请求的关键。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP 服务启动",
			slog.String("addr", cfg.Addr),
			slog.String("env", cfg.Environment),
			slog.String("version", version),
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("HTTP 服务异常退出: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("收到退出信号，开始优雅关闭", slog.Duration("timeout", cfg.ShutdownTimeout))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		// 超时强制关闭，避免进程卡住被 SIGKILL。
		_ = httpServer.Close()
		return fmt.Errorf("优雅关闭超时: %w", err)
	}

	logger.Info("服务已退出")
	return nil
}

// newLogger 根据环境选择日志形态：
//   - development：文本格式 + debug 级别，便于本地阅读
//   - 其他环境：JSON 格式，便于日志采集与检索
func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.Environment == "development" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
