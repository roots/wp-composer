package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/roots/wp-composer/internal/app"
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

	ln, err := socketActivationListener()
	if err != nil {
		return fmt.Errorf("socket activation: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		if ln != nil {
			a.Logger.Info("starting server (socket activated)", "addr", ln.Addr().String())
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("server error: %w", err)
			}
		} else {
			a.Logger.Info("starting server", "addr", a.Config.Server.Addr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("server error: %w", err)
			}
		}
		close(errCh)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		a.Logger.Info("shutting down", "signal", sig.String())
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	a.Logger.Info("server stopped")
	return nil
}

// socketActivationListener returns a net.Listener from a systemd-passed fd,
// or nil if not running under socket activation.
func socketActivationListener() (net.Listener, error) {
	pid, ok := os.LookupEnv("LISTEN_PID")
	if !ok || pid != fmt.Sprintf("%d", os.Getpid()) {
		return nil, nil
	}

	// systemd passes sockets starting at fd 3
	f := os.NewFile(3, "systemd-socket")
	if f == nil {
		return nil, nil
	}
	defer func() { _ = f.Close() }()

	ln, err := net.FileListener(f)
	if err != nil {
		return nil, fmt.Errorf("creating listener from fd 3: %w", err)
	}
	return ln, nil
}
