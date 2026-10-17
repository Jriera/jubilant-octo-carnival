package api

import (
	"github.com/Jriera/jubilant-octo-carnival/internal/ecb"
	"github.com/Jriera/jubilant-octo-carnival/internal/official"
)

// ExtraSource names the source of currencies the ECB doesn't quote.
const (
	ExtraSource      = "NBP"
	ExtraAttribution = " Currencies the ECB does not quote: Narodowy Bank Polski (NBP) tables A (daily) and B (weekly), converted to EUR with the NBP euro rate."
)

type otherSource struct {
	Source string `json:"source"`
	Table  string `json:"table"`          // NBP table A (daily) or B (weekly)
	Date   string `json:"date,omitempty"` // publication date of the table used
}

// extra is what withExtra added to a day.
type extra struct {
	codes map[string]official.ExtraRate
}

func (x extra) has(c string) bool { _, ok := x.codes[c]; return ok }

// withExtra adds the currencies the ECB doesn't quote, from the most recent
// NBP table B, to a copy of day. ECB rates are never replaced.
func (s *Server) withExtra(day ecb.Day) (ecb.Day, extra) {
	if s.Extra == nil {
		return day, extra{}
	}
	more := s.Extra.On(day.Date)
	if len(more) == 0 {
		return day, extra{}
	}
	merged := ecb.Day{Date: day.Date, Rates: make(map[string]float64, len(day.Rates)+len(more))}
	for c, v := range day.Rates {
		merged.Rates[c] = v
	}
	x := extra{codes: map[string]official.ExtraRate{}}
	for c, v := range more {
		if _, ecbHas := day.Rates[c]; !ecbHas {
			merged.Rates[c] = v.PerEUR
			x.codes[c] = v
		}
	}
	return merged, x
}

// others lists which of the returned currencies (and the base) came from the
// extra source. withDate is false for ranges, where the date varies by day.
func (x extra) others(base string, out map[string]float64, withDate bool) map[string]otherSource {
	if len(x.codes) == 0 {
		return nil
	}
	m := map[string]otherSource{}
	add := func(c string) {
		if r, ok := x.codes[c]; ok {
			o := otherSource{Source: ExtraSource, Table: r.Table}
			if withDate {
				o.Date = r.Date
			}
			m[c] = o
		}
	}
	add(base)
	for c := range out {
		add(c)
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func attribution(others map[string]otherSource) string {
	if len(others) > 0 {
		return Attribution + ExtraAttribution
	}
	return Attribution
}

// mergeOthers folds per-day others into one map for range responses.
func mergeOthers(dst, src map[string]otherSource) map[string]otherSource {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]otherSource{}
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (s *Server) latestDay() (ecb.Day, extra, error) {
	d, err := s.Store.Latest()
	if err != nil {
		return d, extra{}, err
	}
	d, x := s.withExtra(d)
	return d, x, nil
}

func (s *Server) dayOn(date string) (ecb.Day, extra, error) {
	d, err := s.Store.On(date)
	if err != nil {
		return d, extra{}, err
	}
	d, x := s.withExtra(d)
	return d, x, nil
}

type extraDay struct {
	ecb.Day
	x extra
}

func (s *Server) daysIn(start, end string) ([]extraDay, error) {
	days, err := s.Store.Range(start, end)
	if err != nil {
		return nil, err
	}
	out := make([]extraDay, len(days))
	for i, d := range days {
		md, x := s.withExtra(d)
		out[i] = extraDay{md, x}
	}
	return out, nil
}
