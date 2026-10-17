package rates

import (
	"errors"
	"testing"

	"github.com/Jriera/jubilant-octo-carnival/internal/ecb"
)

func seed() *Store {
	s := New()
	s.Merge([]ecb.Day{
		{Date: "2026-10-16", Rates: map[string]float64{"USD": 1.165, "JPY": 175.2, "GBP": 0.8674}},
		{Date: "2026-10-14", Rates: map[string]float64{"USD": 1.158, "JPY": 174.1, "GBP": 0.864}},
		{Date: "2026-10-15", Rates: map[string]float64{"USD": 1.16, "JPY": 174.8, "GBP": 0.865}},
	})
	return s
}

func TestLatestAndOn(t *testing.T) {
	s := seed()
	if d, _ := s.Latest(); d.Date != "2026-10-16" {
		t.Fatalf("latest = %s", d.Date)
	}
	// Saturday falls back to Friday's rates.
	if d, _ := s.On("2026-10-17"); d.Date != "2026-10-16" {
		t.Fatalf("On(sat) = %s", d.Date)
	}
	if d, _ := s.On("2026-10-15"); d.Date != "2026-10-15" {
		t.Fatalf("On = %s", d.Date)
	}
	if _, err := s.On("1998-01-01"); !errors.Is(err, ErrBeforeFirstDate) {
		t.Fatalf("expected ErrBeforeFirstDate, got %v", err)
	}
	if _, err := New().Latest(); !errors.Is(err, ErrNoData) {
		t.Fatalf("expected ErrNoData, got %v", err)
	}
}

func TestRange(t *testing.T) {
	days, _ := seed().Range("2026-10-15", "2026-10-20")
	if len(days) != 2 || days[0].Date != "2026-10-15" {
		t.Fatalf("range = %+v", days)
	}
}

func TestMergeSkipsImplausibleDay(t *testing.T) {
	s := seed()
	n := s.Merge([]ecb.Day{{Date: "2026-10-19", Rates: map[string]float64{"USD": 2.5, "JPY": 175, "GBP": 0.87}}})
	if n != 0 {
		t.Fatalf("accepted %d days, want 0", n)
	}
	if d, _ := s.Latest(); d.Date != "2026-10-16" {
		t.Fatalf("latest moved to %s", d.Date)
	}
	// A normal next day is accepted.
	if n := s.Merge([]ecb.Day{{Date: "2026-10-19", Rates: map[string]float64{"USD": 1.17, "JPY": 175, "GBP": 0.87}}}); n != 1 {
		t.Fatalf("accepted %d, want 1", n)
	}
}

func TestCross(t *testing.T) {
	d, _ := seed().Latest()

	eur, err := Cross(d, "EUR", nil)
	if err != nil || eur["USD"] != 1.165 || len(eur) != 3 {
		t.Fatalf("EUR base: %v %v", eur, err)
	}

	usd, err := Cross(d, "USD", []string{"EUR", "JPY"})
	if err != nil {
		t.Fatal(err)
	}
	if usd["EUR"] != 0.858369 || usd["JPY"] != 150.386 {
		t.Fatalf("USD base: %v", usd)
	}

	if _, err := Cross(d, "XXX", nil); !errors.Is(err, ErrUnknownCurrency) {
		t.Fatalf("expected ErrUnknownCurrency, got %v", err)
	}
}
