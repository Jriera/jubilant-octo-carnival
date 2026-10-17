// Command server runs the Exchange Rates API.
//
// Environment:
//
//	PORT                   listen port (default 8080)
//	RAPIDAPI_PROXY_SECRET  when set, /v1 requests must carry this X-RapidAPI-Proxy-Secret
//	ECB_BASE_URL           override the ECB download location (tests, mirrors)
//	REFRESH_INTERVAL       how often to poll the ECB daily file (default 10m)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Jriera/jubilant-octo-carnival/internal/api"
	"github.com/Jriera/jubilant-octo-carnival/internal/ecb"
	"github.com/Jriera/jubilant-octo-carnival/internal/rates"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	port := env("PORT", "8080")
	interval, err := time.ParseDuration(env("REFRESH_INTERVAL", "10m"))
	if err != nil {
		slog.Error("bad REFRESH_INTERVAL", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := rates.New()
	client := ecb.NewClient(os.Getenv("ECB_BASE_URL"))
	go refresh(ctx, client, store, interval)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           (&api.Server{Store: store, ProxySecret: os.Getenv("RAPIDAPI_PROXY_SECRET")}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	slog.Info("listening", "port", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// refresh loads the full history once (retrying until it succeeds), then
// polls the small daily file. Polling is cheap and makes new rates appear
// within one interval of the ECB publishing them (~16:00 CET).
func refresh(ctx context.Context, c *ecb.Client, s *rates.Store, interval time.Duration) {
	backoff := 5 * time.Second
	for s.Len() == 0 {
		days, err := c.History(ctx)
		if err == nil {
			n := s.Merge(days)
			slog.Info("history loaded", "days", n)
			break
		}
		slog.Error("history load failed", "err", err, "retry_in", backoff.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Minute)
	}

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		days, err := c.Daily(ctx)
		if err != nil {
			slog.Warn("daily refresh failed", "err", err)
			continue
		}
		before, _ := s.Latest()
		s.Merge(days)
		if after, _ := s.Latest(); after.Date != before.Date {
			slog.Info("new rates", "date", after.Date)
		}
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
