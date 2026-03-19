package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/roots/wp-packages/internal/app"
)

func ListenAndServe(a *app.App) error {
	router := NewRouter(a)

	csrfProtection := http.NewCrossOriginProtection()
	handler := csrfProtection.Handler(router)

	srv := &http.Server{
		Addr:         a.Config.Server.Addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		a.Logger.Info("starting server", "addr", a.Config.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("server error: %w", err)
		}
		close(errCh)
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-sigCtx.Done():
		a.Logger.Info("shutting down", "cause", context.Cause(sigCtx))
		stop()
	case err := <-errCh:
		if err != nil {
			sentry.CaptureException(err)
			sentry.Flush(2 * time.Second)
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		shutdownErr := fmt.Errorf("shutdown: %w", err)
		sentry.CaptureException(shutdownErr)
		sentry.Flush(2 * time.Second)
		return shutdownErr
	}

	a.Logger.Info("server stopped")
	return nil
}
