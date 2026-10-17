// Package rates keeps every published day in memory and answers lookups
// without locks on the read path.
//
// The full ECB history (1999 onwards, ~30 currencies) is only a few MB, so
// holding it in process is both the simplest and the fastest option.
package rates

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/Jriera/jubilant-octo-carnival/internal/ecb"
)

var (
	ErrNoData          = errors.New("no rates loaded yet")
	ErrBeforeFirstDate = errors.New("date is before the first published rates")
	ErrUnknownCurrency = errors.New("unknown currency")
)

// MaxJump is the largest day-on-day change in a rate that is accepted for a
// new latest day. Larger moves are treated as bad data and the day is skipped.
const MaxJump = 0.25

type snapshot struct {
	days []ecb.Day // sorted by date ascending; EUR=1 added to every day
}

// Store is safe for concurrent use. Writers are serialised; readers never block.
type Store struct {
	mu   sync.Mutex
	snap atomic.Pointer[snapshot]
}

func New() *Store {
	s := &Store{}
	s.snap.Store(&snapshot{})
	return s
}

// Merge adds or replaces days. It returns the number of days accepted.
func (s *Store) Merge(in []ecb.Day) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur := s.snap.Load().days
	byDate := make(map[string]ecb.Day, len(cur)+len(in))
	for _, d := range cur {
		byDate[d.Date] = d
	}
	var latest *ecb.Day
	if len(cur) > 0 {
		latest = &cur[len(cur)-1]
	}

	sorted := append([]ecb.Day(nil), in...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date < sorted[j].Date })

	accepted := 0
	for _, d := range sorted {
		if latest != nil && d.Date > latest.Date {
			if err := plausible(*latest, d); err != nil {
				slog.Warn("skipping implausible day", "date", d.Date, "err", err)
				continue
			}
		}
		day := ecb.Day{Date: d.Date, Rates: make(map[string]float64, len(d.Rates)+1)}
		for k, v := range d.Rates {
			day.Rates[k] = v
		}
		day.Rates["EUR"] = 1
		byDate[d.Date] = day
		if latest == nil || day.Date > latest.Date {
			latest = &day
		}
		accepted++
	}

	days := make([]ecb.Day, 0, len(byDate))
	for _, d := range byDate {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	s.snap.Store(&snapshot{days: days})
	return accepted
}

func plausible(prev, next ecb.Day) error {
	if len(next.Rates) < len(prev.Rates)-1-3 { // prev includes EUR; allow a few to drop out
		return fmt.Errorf("only %d currencies, previous day had %d", len(next.Rates), len(prev.Rates)-1)
	}
	for c, v := range next.Rates {
		if p, ok := prev.Rates[c]; ok && math.Abs(v/p-1) > MaxJump {
			return fmt.Errorf("%s moved from %v to %v", c, p, v)
		}
	}
	return nil
}

// Latest returns the most recent day.
func (s *Store) Latest() (ecb.Day, error) {
	days := s.snap.Load().days
	if len(days) == 0 {
		return ecb.Day{}, ErrNoData
	}
	return days[len(days)-1], nil
}

// On returns the day published on date, or the closest earlier day when
// nothing was published that date (weekends, TARGET holidays).
func (s *Store) On(date string) (ecb.Day, error) {
	days := s.snap.Load().days
	if len(days) == 0 {
		return ecb.Day{}, ErrNoData
	}
	i := sort.Search(len(days), func(i int) bool { return days[i].Date > date })
	if i == 0 {
		return ecb.Day{}, ErrBeforeFirstDate
	}
	return days[i-1], nil
}

// Range returns all published days with start <= date <= end.
func (s *Store) Range(start, end string) ([]ecb.Day, error) {
	days := s.snap.Load().days
	if len(days) == 0 {
		return nil, ErrNoData
	}
	lo := sort.Search(len(days), func(i int) bool { return days[i].Date >= start })
	hi := sort.Search(len(days), func(i int) bool { return days[i].Date > end })
	return days[lo:hi], nil
}

// Len reports how many days are loaded.
func (s *Store) Len() int { return len(s.snap.Load().days) }

// Cross converts the EUR-based rates of day into rates for base.
// For base EUR the published values are returned unchanged.
func Cross(day ecb.Day, base string, symbols []string) (map[string]float64, error) {
	b, ok := day.Rates[base]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownCurrency, base)
	}
	out := make(map[string]float64)
	add := func(c string, v float64) {
		if c == base {
			return
		}
		if base == "EUR" {
			out[c] = v
		} else {
			out[c] = round6(v / b)
		}
	}
	if len(symbols) == 0 {
		for c, v := range day.Rates {
			add(c, v)
		}
		return out, nil
	}
	for _, c := range symbols {
		v, ok := day.Rates[c]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownCurrency, c)
		}
		add(c, v)
	}
	return out, nil
}

// round6 rounds to 6 significant digits, enough precision for derived cross rates.
func round6(x float64) float64 {
	if x == 0 {
		return 0
	}
	p := math.Pow(10, 5-math.Floor(math.Log10(math.Abs(x))))
	return math.Round(x*p) / p
}
