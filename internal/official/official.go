// Package official holds rate tables that national authorities publish for
// tax, invoicing and customs (NBP, CNB, MNB, HMRC), and the rules that say
// which table applies to a given date.
//
// Each source keeps its whole loaded history in memory, like the ECB store:
// a few years of one table a day is well under a megabyte.
package official

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"
)

var (
	ErrNoData          = errors.New("rates not loaded yet")
	ErrNotPublished    = errors.New("no table published for that date")
	ErrUnknownCurrency = errors.New("currency not quoted in that table")
)

// Quote is one published rate, exactly as the authority states it.
type Quote struct {
	Value float64
	// Units is how many units of the foreign currency Value is for
	// (CNB quotes 100 HUF, MNB 100 JPY). Always >= 1.
	Units float64
	// Inverse means Value is foreign currency per 1 unit of local currency
	// (HMRC: "1 GBP = 1.3317 USD"). Otherwise Value is local per Units foreign.
	Inverse bool
}

// PerUnit is local currency per 1 unit of foreign currency.
func (q Quote) PerUnit() float64 {
	if q.Inverse {
		return 1 / q.Value
	}
	return q.Value / q.Units
}

// Convert turns an amount of foreign currency into local currency the way the
// authority's own quote implies (divide for HMRC, multiply for the others),
// so results match a hand calculation from the published table.
func (q Quote) Convert(amount float64) float64 {
	if q.Inverse {
		return amount / q.Value
	}
	return amount * q.Value / q.Units
}

// Table is one publication.
type Table struct {
	Date    string // publication date, or first day of the period (YYYY-MM-DD)
	ValidTo string // last day of the period for monthly tables; empty for daily
	ID      string // the authority's own table number, when it has one
	Rates   map[string]Quote
}

// Series is the loaded history of one source. Safe for concurrent use;
// readers never block.
type Series struct {
	mu   sync.Mutex
	snap atomic.Pointer[[]Table]
}

func NewSeries() *Series {
	s := &Series{}
	s.snap.Store(&[]Table{})
	return s
}

// Merge adds or replaces tables by date and returns how many it took.
func (s *Series) Merge(in []Table) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	byDate := map[string]Table{}
	for _, t := range *s.snap.Load() {
		byDate[t.Date] = t
	}
	n := 0
	for _, t := range in {
		if t.Date == "" || len(t.Rates) == 0 {
			continue
		}
		byDate[t.Date] = t
		n++
	}
	out := make([]Table, 0, len(byDate))
	for _, t := range byDate {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	s.snap.Store(&out)
	return n
}

func (s *Series) Len() int { return len(*s.snap.Load()) }

// First and Last return the loaded date range.
func (s *Series) First() (Table, bool) {
	t := *s.snap.Load()
	if len(t) == 0 {
		return Table{}, false
	}
	return t[0], true
}

func (s *Series) Last() (Table, bool) {
	t := *s.snap.Load()
	if len(t) == 0 {
		return Table{}, false
	}
	return t[len(t)-1], true
}

// OnOrBefore returns the latest table dated date or earlier.
func (s *Series) OnOrBefore(date string) (Table, error) { return s.find(date, false) }

// Before returns the latest table dated strictly before date.
func (s *Series) Before(date string) (Table, error) { return s.find(date, true) }

func (s *Series) find(date string, strict bool) (Table, error) {
	t := *s.snap.Load()
	if len(t) == 0 {
		return Table{}, ErrNoData
	}
	i := sort.Search(len(t), func(i int) bool {
		if strict {
			return t[i].Date >= date
		}
		return t[i].Date > date
	})
	if i == 0 {
		return Table{}, ErrNotPublished
	}
	return t[i-1], nil
}

// Covering returns the period table whose Date..ValidTo contains date.
func (s *Series) Covering(date string) (Table, error) {
	t, err := s.OnOrBefore(date)
	if err != nil {
		return Table{}, err
	}
	if t.ValidTo != "" && date > t.ValidTo {
		return Table{}, ErrNotPublished
	}
	return t, nil
}
