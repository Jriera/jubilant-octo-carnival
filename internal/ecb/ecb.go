// Package ecb fetches and parses the ECB euro foreign exchange reference rates.
//
// Source: https://www.ecb.europa.eu/stats/policy_and_exchange_rates/euro_reference_exchange_rates/
// Reuse is free of charge, including commercially, provided the source is
// cited ("Source: ECB statistics") and the figures are not altered.
package ecb

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	DefaultBaseURL = "https://www.ecb.europa.eu/stats/eurofxref"
	DailyPath      = "/eurofxref-daily.xml"
	HistoryPath    = "/eurofxref-hist.xml"
)

// Day holds the reference rates published for one date. Rates are units of
// currency per 1 EUR, exactly as published.
type Day struct {
	Date  string // YYYY-MM-DD
	Rates map[string]float64
}

type envelope struct {
	Cube struct {
		Days []struct {
			Time  string `xml:"time,attr"`
			Rates []struct {
				Currency string `xml:"currency,attr"`
				Rate     string `xml:"rate,attr"`
			} `xml:"Cube"`
		} `xml:"Cube"`
	} `xml:"Cube"`
}

// Parse reads an ECB eurofxref XML document (daily, 90-day or full history).
func Parse(r io.Reader) ([]Day, error) {
	var env envelope
	if err := xml.NewDecoder(r).Decode(&env); err != nil {
		return nil, fmt.Errorf("decode ecb xml: %w", err)
	}
	days := make([]Day, 0, len(env.Cube.Days))
	for _, d := range env.Cube.Days {
		if _, err := time.Parse(time.DateOnly, d.Time); err != nil {
			return nil, fmt.Errorf("bad date %q: %w", d.Time, err)
		}
		rates := make(map[string]float64, len(d.Rates))
		for _, r := range d.Rates {
			v, err := strconv.ParseFloat(r.Rate, 64)
			if err != nil || v <= 0 || len(r.Currency) != 3 {
				return nil, fmt.Errorf("bad rate %s=%q on %s", r.Currency, r.Rate, d.Time)
			}
			rates[r.Currency] = v
		}
		if len(rates) > 0 {
			days = append(days, Day{Date: d.Time, Rates: rates})
		}
	}
	if len(days) == 0 {
		return nil, fmt.Errorf("ecb xml contained no rates")
	}
	return days, nil
}

// Client downloads ECB files.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Daily(ctx context.Context) ([]Day, error)   { return c.get(ctx, DailyPath) }
func (c *Client) History(ctx context.Context) ([]Day, error) { return c.get(ctx, HistoryPath) }

func (c *Client) get(ctx context.Context, path string) ([]Day, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "exchange-rates-api/1.0")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
	}
	return Parse(resp.Body)
}
