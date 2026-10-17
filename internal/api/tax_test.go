package api

import (
	"testing"
	"time"

	"github.com/Jriera/jubilant-octo-carnival/internal/official"
	"github.com/Jriera/jubilant-octo-carnival/internal/rates"
)

func taxServer() *Server {
	cs := official.Countries()
	for _, c := range cs {
		c.Series = official.NewSeries()
		switch c.Code {
		case "PL":
			c.Series.Merge([]official.Table{
				{Date: "2026-10-14", ID: "199/A/NBP/2026", Rates: map[string]official.Quote{"USD": {Value: 3.6512, Units: 1}}},
				{Date: "2026-10-15", ID: "200/A/NBP/2026", Rates: map[string]official.Quote{"USD": {Value: 3.6601, Units: 1}}},
			})
		case "CZ":
			c.Series.Merge([]official.Table{
				{Date: "2026-10-15", ID: "199/2026", Rates: map[string]official.Quote{"HUF": {Value: 6.143, Units: 100}}},
			})
		case "GB":
			c.Series.Merge([]official.Table{
				{Date: "2026-10-01", ValidTo: "2026-10-31", ID: "October 2026", Rates: map[string]official.Quote{"USD": {Value: 1.34, Units: 1, Inverse: true}}},
			})
		}
	}
	now := func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) }
	return &Server{Store: rates.New(), Tax: cs, Now: now}
}

func TestTaxRatePoland(t *testing.T) {
	h := taxServer().Handler()
	code, body := get(t, h, "/v1/tax-rates?country=pl&currency=usd&date=2026-10-15&amount=100", nil)
	if code != 200 {
		t.Fatalf("%d %v", code, body)
	}
	// Tax point 15th: the table from the 14th applies.
	if body["table"] != "199/A/NBP/2026" || body["rate_date"] != "2026-10-14" || body["rate"] != 3.6512 ||
		body["result"] != 365.12 || body["applied_date"] != "2026-10-15" || body["local_currency"] != "PLN" {
		t.Fatalf("%v", body)
	}
	if body["published"].(map[string]any)["quote"] != "PLN per 1 USD" || body["legal_basis"] == "" {
		t.Fatalf("%v", body)
	}
	// Invoice issued before the tax point moves the anchor.
	_, body = get(t, h, "/v1/tax-rates?country=PL&currency=USD&date=2026-10-20&invoice_date=2026-10-15", nil)
	if body["applied_date"] != "2026-10-15" || body["table"] != "199/A/NBP/2026" {
		t.Fatalf("%v", body)
	}
}

func TestTaxRateUnitsAndInverse(t *testing.T) {
	h := taxServer().Handler()
	_, body := get(t, h, "/v1/tax-rates?country=CZ&currency=HUF&date=2026-10-15&amount=1000", nil)
	if body["rate"] != 0.06143 || body["result"] != 61.43 || body["published"].(map[string]any)["quote"] != "CZK per 100 HUF" {
		t.Fatalf("%v", body)
	}
	_, body = get(t, h, "/v1/tax-rates?country=UK&currency=USD&date=2026-10-10&amount=134", nil)
	if body["country"] != "GB" || body["result"] != 100.0 || body["rate"] != 0.74626866 ||
		body["valid_to"] != "2026-10-31" || body["published"].(map[string]any)["quote"] != "USD per 1 GBP" {
		t.Fatalf("%v", body)
	}
}

func TestTaxRateErrors(t *testing.T) {
	h := taxServer().Handler()
	cases := map[string]int{
		"/v1/tax-rates?country=FR&currency=USD&date=2026-10-15":                         400,
		"/v1/tax-rates?country=PL&currency=PLN&date=2026-10-15":                         400,
		"/v1/tax-rates?country=PL&currency=USD":                                         400,
		"/v1/tax-rates?country=PL&currency=USD&date=2026-12-01":                         400, // future
		"/v1/tax-rates?country=PL&currency=USD&date=2026-10-15&invoice_date=15.10.2026": 400,
		"/v1/tax-rates?country=PL&currency=USD&date=2026-10-15&amount=x":                400,
		"/v1/tax-rates?country=PL&currency=USD&date=2026-10-14":                         404, // before first table
		"/v1/tax-rates?country=PL&currency=JPY&date=2026-10-15":                         404,
		"/v1/tax-rates?country=GB&currency=USD&date=2026-09-30":                         404,
		"/v1/tax-rates?country=HU&currency=EUR&date=2026-10-15":                         503, // not loaded
	}
	for url, want := range cases {
		if code, body := get(t, h, url, nil); code != want {
			t.Errorf("%s: %d %v, want %d", url, code, body, want)
		}
	}
}

func TestTaxRateProvisional(t *testing.T) {
	h := taxServer().Handler()
	// CZ uses the same day's fixing; the 16th isn't loaded yet.
	_, body := get(t, h, "/v1/tax-rates?country=CZ&currency=HUF&date=2026-10-16", nil)
	if body["provisional"] != true {
		t.Fatalf("%v", body)
	}
	// PL on the 16th wants the 15th, which is loaded.
	_, body = get(t, h, "/v1/tax-rates?country=PL&currency=USD&date=2026-10-16", nil)
	if _, ok := body["provisional"]; ok {
		t.Fatalf("%v", body)
	}
}

func TestTaxCountries(t *testing.T) {
	code, body := get(t, taxServer().Handler(), "/v1/tax-rates/countries", nil)
	list := body["countries"].([]any)
	if code != 200 || len(list) != 4 {
		t.Fatalf("%d %v", code, body)
	}
	pl := list[0].(map[string]any)
	if pl["country"] != "PL" || pl["from"] != "2026-10-14" || pl["loaded"] != true {
		t.Fatalf("%v", pl)
	}
	gb := list[3].(map[string]any)
	if gb["to"] != "2026-10-31" {
		t.Fatalf("%v", gb)
	}
}
