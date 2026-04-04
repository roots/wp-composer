package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/roots/wp-packages/internal/app"
)

// systemdListener returns a net.Listener from a socket fd passed by systemd
// socket activation (sd_listen_fds protocol). Returns nil if not running
// under socket activation.
func systemdListener() (net.Listener, error) {
	pid, err := strconv.Atoi(os.Getenv("LISTEN_PID"))
	if err != nil || pid != os.Getpid() {
		return nil, nil
	}
	nfds, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil || nfds < 1 {
		return nil, nil
	}

	f := os.NewFile(3, "systemd-socket")
	ln, err := net.FileListener(f)
	_ = f.Close()
	if err != nil {
		return nil, fmt.Errorf("creating listener from systemd fd: %w", err)
	}

	_ = os.Unsetenv("LISTEN_PID")
	_ = os.Unsetenv("LISTEN_FDS")
	return ln, nil
}

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

	ln, err := systemdListener()
	if err != nil {
		return err
	}

	if ln != nil {
		a.Logger.Info("using systemd socket activation")
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("server error: %w", err)
			}
			close(errCh)
		}()
	} else {
		a.Logger.Info("starting server", "addr", a.Config.Server.Addr)
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("server error: %w", err)
			}
			close(errCh)
		}()
	}

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
