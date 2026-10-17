// Command server runs the Exchange Rates API.
//
// Environment:
//
//	PORT                   listen port (default 8080)
//	RAPIDAPI_PROXY_SECRET  secret RapidAPI sends in X-RapidAPI-Proxy-Secret
//	GATEWAY_SECRET         secret the Zuplo gateway sends in X-Gateway-Secret
//	                       (when either is set, /v1 requests must carry one of them)
//	ECB_BASE_URL           override the ECB download location (tests, mirrors)
//	REFRESH_INTERVAL       how often to poll the ECB daily file (default 10m)
//	TAX_COUNTRIES          tax-rate countries to load (default PL,CZ,HU,GB; "none" disables)
//	TAX_HISTORY_YEARS      years of tax-rate history to load (default 10; HMRC starts 2021)
//	TAX_REFRESH_INTERVAL   how often to poll the tax-rate and NBP sources (default 30m)
//	EXTRA_CURRENCIES       "nbp" (default) adds NBP table A/B currencies the ECB lacks; "none" disables
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

	tax, extra, err := startOfficial(ctx)
	if err != nil {
		slog.Error("bad configuration", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           (&api.Server{Store: store, Tax: tax, Extra: extra, ProxySecret: os.Getenv("RAPIDAPI_PROXY_SECRET"), GatewaySecret: os.Getenv("GATEWAY_SECRET")}).Handler(),
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

// startOfficial starts one loader per enabled national source. Each loads
// independently, so a slow or failing bank never delays the others or the
// ECB data. NBP table A is loaded once and shared by the Poland tax rule and
// the extra currencies.
func startOfficial(ctx context.Context) ([]*official.Country, *official.Extra, error) {
	years, err := strconv.Atoi(env("TAX_HISTORY_YEARS", "10"))
	if err != nil || years < 1 {
		return nil, nil, errors.New("TAX_HISTORY_YEARS must be a positive whole number")
	}
	interval, err := time.ParseDuration(env("TAX_REFRESH_INTERVAL", "30m"))
	if err != nil {
		return nil, nil, err
	}
	from := time.Now().UTC().AddDate(-years, 0, 0)

	enabled := map[string]bool{}
	if want := strings.ToUpper(env("TAX_COUNTRIES", "PL,CZ,HU,GB")); want != "NONE" {
		for _, c := range strings.Split(want, ",") {
			c = strings.TrimSpace(c)
			switch c {
			case "PL", "CZ", "HU", "GB":
				enabled[c] = true
			default:
				return nil, nil, errors.New("unknown TAX_COUNTRIES entry " + c)
			}
		}
	}
	var extra *official.Extra
	switch strings.ToLower(env("EXTRA_CURRENCIES", "nbp")) {
	case "nbp":
		extra = &official.Extra{B: official.NewSeries()}
	case "none":
	default:
		return nil, nil, errors.New("EXTRA_CURRENCIES must be nbp or none")
	}

	var nbpA *official.Series
	if enabled["PL"] || extra != nil {
		nbpA = official.NewSeries()
		go load(ctx, "NBP-A", nbpA, official.NewNBP(""), from, interval)
	}
	if extra != nil {
		extra.A = nbpA
		go load(ctx, "NBP-B", extra.B, official.NewNBPB(""), from, interval)
	}

	fetchers := map[string]official.Fetcher{
		"CZ": official.NewCNB(""),
		"HU": official.NewMNB(""),
		"GB": official.NewHMRC(""),
	}
	var tax []*official.Country
	for _, c := range official.Countries() {
		if !enabled[c.Code] {
			continue
		}
		if c.Code == "PL" {
			c.Series = nbpA
		} else {
			c.Series = official.NewSeries()
			go load(ctx, c.Source, c.Series, fetchers[c.Code], from, interval)
		}
		tax = append(tax, c)
	}
	return tax, extra, nil
}

func load(ctx context.Context, name string, series *official.Series, f official.Fetcher, from time.Time, interval time.Duration) {
	log := slog.With("source", name)
	backoff := 10 * time.Second
	for series.Len() == 0 {
		tables, err := f.History(ctx, from)
		if err == nil && len(tables) > 0 {
			log.Info("history loaded", "tables", series.Merge(tables))
			break
		}
		if ctx.Err() != nil {
			return
		}
		log.Error("history load failed", "err", err, "retry_in", backoff.String())
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
			log.Warn("refresh failed", "err", err)
			continue
		}
		before, _ := series.Last()
		series.Merge(tables)
		if after, _ := series.Last(); after.Date != before.Date {
			log.Info("new table", "date", after.Date, "table", after.ID)
		}
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
