// Command server runs the Exchange Rates API.
//
// Environment:
//
//	PORT                   listen port (default 8080)
//	RAPIDAPI_PROXY_SECRET  when set, /v1 requests must carry this X-RapidAPI-Proxy-Secret
//	ECB_BASE_URL           override the ECB download location (tests, mirrors)
//	REFRESH_INTERVAL       how often to poll the ECB daily file (default 10m)
//	TAX_COUNTRIES          tax-rate countries to load (default PL,CZ,HU,GB; "none" disables)
//	TAX_HISTORY_YEARS      years of tax-rate history to load (default 10; HMRC starts 2021)
//	TAX_REFRESH_INTERVAL   how often to poll the tax-rate sources (default 30m)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jriera/jubilant-octo-carnival/internal/api"
	"github.com/Jriera/jubilant-octo-carnival/internal/ecb"
	"github.com/Jriera/jubilant-octo-carnival/internal/official"
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

	tax, err := startTax(ctx)
	if err != nil {
		slog.Error("bad tax configuration", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           (&api.Server{Store: store, Tax: tax, ProxySecret: os.Getenv("RAPIDAPI_PROXY_SECRET")}).Handler(),
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

// startTax starts one loader per enabled country. Each loads independently,
// so a slow or failing central bank never delays the others or the ECB data.
func startTax(ctx context.Context) ([]*official.Country, error) {
	want := strings.ToUpper(env("TAX_COUNTRIES", "PL,CZ,HU,GB"))
	if want == "NONE" {
		return nil, nil
	}
	years, err := strconv.Atoi(env("TAX_HISTORY_YEARS", "10"))
	if err != nil || years < 1 {
		return nil, errors.New("TAX_HISTORY_YEARS must be a positive whole number")
	}
	interval, err := time.ParseDuration(env("TAX_REFRESH_INTERVAL", "30m"))
	if err != nil {
		return nil, err
	}
	fetchers := map[string]official.Fetcher{
		"PL": official.NewNBP(""),
		"CZ": official.NewCNB(""),
		"HU": official.NewMNB(""),
		"GB": official.NewHMRC(""),
	}
	enabled := map[string]bool{}
	for _, c := range strings.Split(want, ",") {
		c = strings.TrimSpace(c)
		if _, ok := fetchers[c]; !ok {
			return nil, errors.New("unknown TAX_COUNTRIES entry " + c)
		}
		enabled[c] = true
	}
	from := time.Now().UTC().AddDate(-years, 0, 0)
	var out []*official.Country
	for _, c := range official.Countries() {
		if !enabled[c.Code] {
			continue
		}
		c.Series = official.NewSeries()
		go refreshTax(ctx, c, fetchers[c.Code], from, interval)
		out = append(out, c)
	}
	return out, nil
}

func refreshTax(ctx context.Context, c *official.Country, f official.Fetcher, from time.Time, interval time.Duration) {
	log := slog.With("source", c.Source)
	backoff := 10 * time.Second
	for c.Series.Len() == 0 {
		tables, err := f.History(ctx, from)
		if err == nil && len(tables) > 0 {
			log.Info("tax history loaded", "tables", c.Series.Merge(tables))
			break
		}
		if ctx.Err() != nil {
			return
		}
		log.Error("tax history load failed", "err", err, "retry_in", backoff.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 10*time.Minute)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		tables, err := f.Recent(ctx)
		if err != nil {
			log.Warn("tax refresh failed", "err", err)
			continue
		}
		before, _ := c.Series.Last()
		c.Series.Merge(tables)
		if after, _ := c.Series.Last(); after.Date != before.Date {
			log.Info("new tax table", "date", after.Date, "table", after.ID)
		}
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
