package app

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/roots/wp-packages/internal/blog"
	"github.com/roots/wp-packages/internal/config"
	"github.com/roots/wp-packages/internal/db"
	"github.com/roots/wp-packages/internal/packagist"
)

type App struct {
	Config    *config.Config
	DB        *sql.DB
	Logger    *slog.Logger
	Packagist *packagist.DownloadsCache
	Blog      *blog.PostsCache
}

func New(cfg *config.Config) (*App, error) {
	logger := newLogger(cfg)

	if cfg.SentryDSN != "" {
		err := sentry.Init(sentry.ClientOptions{
			Dsn:              cfg.SentryDSN,
			Environment:      cfg.Env,
			TracesSampleRate: 0.1,
		})
		if err != nil {
			logger.Warn("sentry initialization failed", "error", err)
		} else {
			logger.Info("sentry initialized")
		}
	}

	database, err := db.Open(cfg.DB.Path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	return &App{
		Config:    cfg,
		DB:        database,
		Logger:    logger,
		Packagist: packagist.NewDownloadsCache(logger),
		Blog:      blog.NewPostsCache(logger),
	}, nil
}

func (a *App) Close() error {
	sentry.Flush(2 * time.Second)
	if a.DB != nil {
		return a.DB.Close()
	}
	return nil
}

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Env == "production" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}
