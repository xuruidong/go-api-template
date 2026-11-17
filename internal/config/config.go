// Package config 负责从环境变量加载并校验服务配置。
//
// 配置只从环境变量读取，不读配置文件：
// 这样同一份镜像可以通过部署平台注入不同的值，
// 也符合 12-Factor App 的约定。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Config 是服务的全部可配置项。
type Config struct {
	// Addr 是 HTTP 监听地址，例如 ":8080"。
	Addr string
	// AuthToken 是模板内置的 Bearer Token；生产环境请替换为真实鉴权逻辑。
	AuthToken string
	// Environment 取值 development / staging / production，影响日志格式与默认级别。
	Environment string
	// LogLevel 为 debug / info / warn / error。
	LogLevel slog.Level
	// ShutdownTimeout 是优雅关闭等待的最长时间。
	ShutdownTimeout time.Duration

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

// Load 读取环境变量并校验。
//
// 缺少必填项时返回错误，让进程在启动阶段就失败（fail fast），
// 而不是带着错误配置对外提供服务。
func Load() (Config, error) {
	cfg := Config{
		Addr:              getenv("HTTP_ADDR", ":8080"),
		AuthToken:         os.Getenv("AUTH_TOKEN"),
		Environment:       getenv("APP_ENV", "development"),
		ShutdownTimeout:   getDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		ReadHeaderTimeout: getDuration("READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       getDuration("READ_TIMEOUT", 15*time.Second),
		WriteTimeout:      getDuration("WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:       getDuration("IDLE_TIMEOUT", 60*time.Second),
	}

	level, err := parseLevel(getenv("LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = level

	if cfg.AuthToken == "" {
		return Config{}, errors.New("AUTH_TOKEN 未设置：请通过环境变量提供 Bearer Token（本地开发可用 make run 自动注入 dev-token）")
	}
	if len(cfg.AuthToken) < 8 {
		return Config{}, errors.New("AUTH_TOKEN 过短：至少需要 8 个字符")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func parseLevel(v string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToUpper(v))); err != nil {
		return 0, fmt.Errorf("无效的 LOG_LEVEL %q（可选 debug/info/warn/error）: %w", v, err)
	}
	return level, nil
}
