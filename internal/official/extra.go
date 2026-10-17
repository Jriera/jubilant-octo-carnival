package official

import (
	"math"
	"time"
)

// MaxExtraAge is how old a weekly table B may be and still be served next to
// the ECB's daily rates: one week plus a holiday.
const MaxExtraAge = 9 * 24 * time.Hour

// Extra turns NBP rates (PLN per unit) into EUR-based rates using the NBP
// table A euro rate from the same day, so currencies the ECB doesn't quote can
// sit alongside ECB rates. Table A (daily) wins over table B (weekly).
type Extra struct {
	A, B *Series
}

// ExtraRate is one currency's rate per 1 EUR and the NBP table it came from.
type ExtraRate struct {
	PerEUR float64
	Date   string
	Table  string // "A" or "B"
}

// On returns units of each NBP currency per 1 EUR for date. Tables older
// than MaxExtraAge are ignored.
func (e *Extra) On(date string) map[string]ExtraRate {
	if e == nil {
		return nil
	}
	out := map[string]ExtraRate{}
	e.add(out, e.B, "B", date)
	e.add(out, e.A, "A", date) // overwrites B where both quote a currency
	return out
}

func (e *Extra) add(out map[string]ExtraRate, s *Series, name, date string) {
	if s == nil || e.A == nil {
		return
	}
	t, err := s.OnOrBefore(date)
	if err != nil {
		return
	}
	d, _ := time.Parse(time.DateOnly, date)
	td, _ := time.Parse(time.DateOnly, t.Date)
	if d.Sub(td) > MaxExtraAge {
		return
	}
	a, err := e.A.OnOrBefore(t.Date)
	if err != nil {
		return
	}
	eur, has := a.Rates["EUR"]
	if !has {
		return
	}
	plnPerEUR := eur.PerUnit()
	for c, q := range t.Rates {
		if v := q.PerUnit(); v > 0 && c != "EUR" {
			out[c] = ExtraRate{PerEUR: sig6(plnPerEUR / v), Date: t.Date, Table: name}
		}
	}
	out["PLN"] = ExtraRate{PerEUR: sig6(plnPerEUR), Date: a.Date, Table: "A"}
}

func sig6(x float64) float64 {
	if x == 0 {
		return 0
	}
	p := math.Pow(10, 5-math.Floor(math.Log10(math.Abs(x))))
	return math.Round(x*p) / p
}
