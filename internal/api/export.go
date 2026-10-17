package api

import (
	"encoding/csv"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jriera/jubilant-octo-carnival/internal/ecb"
	"github.com/Jriera/jubilant-octo-carnival/internal/rates"
)

type dayT = ecb.Day

// MaxSeriesDays bounds /v1/timeseries. Ten years of ~30 currencies is about
// 80k points, which still encodes in a few milliseconds.
const MaxSeriesDays = 3660

// wantCSV reports whether the caller asked for CSV, by format=csv or an
// Accept header that prefers text/csv.
func wantCSV(r *http.Request) bool {
	if f := r.URL.Query().Get("format"); f != "" {
		return strings.EqualFold(f, "csv")
	}
	return strings.HasPrefix(r.Header.Get("Accept"), "text/csv")
}

// validFormat rejects unknown format values so typos don't silently return JSON.
func validFormat(w http.ResponseWriter, r *http.Request) bool {
	switch strings.ToLower(r.URL.Query().Get("format")) {
	case "", "json", "csv":
		return true
	}
	writeErr(w, http.StatusBadRequest, "format must be json or csv")
	return false
}

type csvRow struct {
	date, currency, source string
	rate                   float64
}

// writeCSV writes long-format rows (one rate per line), which spreadsheets and
// pivot tables read without reshaping. Rows are sorted by date then currency.
func writeCSV(w http.ResponseWriter, filename, base string, derived bool, rows []csvRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].date != rows[j].date {
			return rows[i].date < rows[j].date
		}
		return rows[i].currency < rows[j].currency
	})
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	attr := Attribution
	for _, r := range rows {
		if r.source == ExtraSource {
			attr += ExtraAttribution
			break
		}
	}
	w.Header().Set("X-Attribution", attr)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"date", "base", "currency", "rate", "derived", "source"})
	d := strconv.FormatBool(derived)
	for _, r := range rows {
		_ = cw.Write([]string{r.date, base, r.currency, strconv.FormatFloat(r.rate, 'f', -1, 64), d, r.source})
	}
	cw.Flush()
}

// dayRows names the source per row: ECB, or NBP for currencies withExtra
// added (and for every row when the base itself came from NBP).
func dayRows(date, base string, rs map[string]float64, x extra) []csvRow {
	rows := make([]csvRow, 0, len(rs))
	for c, v := range rs {
		src := Source
		if x.has(c) || x.has(base) {
			src = ExtraSource
		}
		rows = append(rows, csvRow{date, c, src, v})
	}
	return rows
}

type point struct {
	Date string  `json:"date"`
	Rate float64 `json:"rate"`
}

type seriesStats struct {
	Start         float64 `json:"start"`
	End           float64 `json:"end"`
	Min           float64 `json:"min"`
	Max           float64 `json:"max"`
	Change        float64 `json:"change"`
	ChangePercent float64 `json:"change_percent"`
}

type seriesResponse struct {
	Source       string                 `json:"source"`
	Attribution  string                 `json:"attribution"`
	Base         string                 `json:"base"`
	Start        string                 `json:"start"`
	End          string                 `json:"end"`
	Derived      bool                   `json:"derived"`
	Series       map[string][]point     `json:"series"`
	Stats        map[string]seriesStats `json:"stats"`
	OtherSources map[string]otherSource `json:"other_sources,omitempty"`
}

// timeseries returns one ordered array per currency, plus start/end/min/max
// and change over the range: the shape charting libraries expect.
func (s *Server) timeseries(w http.ResponseWriter, r *http.Request) {
	if !validFormat(w, r) {
		return
	}
	base, symbols, ok := parseBaseSymbols(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	start, end := q.Get("start"), q.Get("end")
	if !validDate(start) || !validDate(end) {
		writeErr(w, http.StatusBadRequest, "start and end are required as YYYY-MM-DD")
		return
	}
	st, _ := time.Parse(time.DateOnly, start)
	en, _ := time.Parse(time.DateOnly, end)
	if en.Before(st) {
		writeErr(w, http.StatusBadRequest, "end is before start")
		return
	}
	if en.Sub(st) > MaxSeriesDays*24*time.Hour {
		writeErr(w, http.StatusBadRequest, "range is limited to 3660 days (10 years) per request")
		return
	}
	days, err := s.daysIn(start, end)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	// Validate requested symbols against the latest day so a typo is a 400,
	// not an empty series.
	if latest, _, err := s.latestDay(); err == nil {
		if _, err := rates.Cross(latest, base, symbols); err != nil && !earlierOnly(days, base, symbols) {
			s.storeErr(w, err)
			return
		}
	}

	series := map[string][]point{}
	var rows []csvRow
	var others map[string]otherSource
	for _, d := range days {
		out, err := rates.Cross(d.Day, base, symbols)
		if errors.Is(err, rates.ErrUnknownCurrency) {
			out = crossAvailable(d.Day, base, symbols)
		} else if err != nil {
			s.storeErr(w, err)
			return
		}
		for c, v := range out {
			series[c] = append(series[c], point{d.Date, v})
		}
		rows = append(rows, dayRows(d.Date, base, out, d.x)...)
		others = mergeOthers(others, d.x.others(base, out, false))
	}
	w.Header().Set("Cache-Control", cacheFor(end, s.now()))
	if wantCSV(r) {
		writeCSV(w, "timeseries_"+base+"_"+start+"_"+end+".csv", base, base != "EUR", rows)
		return
	}
	stats := make(map[string]seriesStats, len(series))
	for c, pts := range series {
		st := seriesStats{Start: pts[0].Rate, End: pts[len(pts)-1].Rate, Min: pts[0].Rate, Max: pts[0].Rate}
		for _, p := range pts {
			st.Min = min(st.Min, p.Rate)
			st.Max = max(st.Max, p.Rate)
		}
		st.Change = round(st.End-st.Start, 6)
		st.ChangePercent = round((st.End-st.Start)/st.Start*100, 4)
		stats[c] = st
	}
	writeJSON(w, seriesResponse{
		Source: Source, Attribution: attribution(others), Base: base, Start: start, End: end,
		Derived: base != "EUR", Series: series, Stats: stats, OtherSources: others,
	})
}

// earlierOnly reports whether every requested currency (and the base) is
// quoted on at least one day of the range, which covers currencies that were
// retired before the latest publication (e.g. a range in 2005 asking for ROL).
func earlierOnly(days []extraDay, base string, symbols []string) bool {
	need := append([]string{base}, symbols...)
	for _, c := range need {
		found := false
		for _, d := range days {
			if _, ok := d.Rates[c]; ok {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// crossAvailable is Cross restricted to the currencies quoted that day.
func crossAvailable(d dayT, base string, symbols []string) map[string]float64 {
	if _, ok := d.Rates[base]; !ok {
		return nil
	}
	var have []string
	for _, c := range symbols {
		if _, ok := d.Rates[c]; ok {
			have = append(have, c)
		}
	}
	if len(have) == 0 {
		return nil
	}
	out, _ := rates.Cross(d, base, have)
	return out
}
