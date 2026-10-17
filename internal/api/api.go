// Package api serves exchange rates over HTTP. Every request is answered from
// memory; nothing on the request path talks to the ECB.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jriera/jubilant-octo-carnival/internal/ecb"
	"github.com/Jriera/jubilant-octo-carnival/internal/official"
	"github.com/Jriera/jubilant-octo-carnival/internal/rates"
)

const (
	Source       = "ECB"
	Attribution  = "Source: ECB statistics. Reference rates for information purposes only."
	MaxRangeDays = 366
	// StaleAfter: the ECB publishes on TARGET working days, so a gap of up to
	// four days (long Easter weekend) is normal.
	StaleAfter = 4 * 24 * time.Hour
)

type Server struct {
	Store       *rates.Store
	Tax         []*official.Country // tax and invoicing rules; optional
	Extra       *official.Extra     // currencies the ECB doesn't quote; optional
	ProxySecret string              // when set, requests must carry X-RapidAPI-Proxy-Secret
	Now         func() time.Time    // for tests
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", s.ready)
	mux.Handle("GET /v1/latest", s.auth(s.latest))
	mux.Handle("GET /v1/convert", s.auth(s.convert))
	mux.Handle("GET /v1/history", s.auth(s.history))
	mux.Handle("GET /v1/timeseries", s.auth(s.timeseries))
	mux.Handle("GET /v1/currencies", s.auth(s.currencies))
	mux.Handle("GET /v1/tax-rates", s.auth(s.taxRate))
	mux.Handle("GET /v1/tax-rates/countries", s.auth(s.taxCountries))
	return mux
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.ProxySecret != "" {
			got := r.Header.Get("X-RapidAPI-Proxy-Secret")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.ProxySecret)) != 1 {
				writeErr(w, http.StatusForbidden, "forbidden")
				return
			}
		}
		h(w, r)
	})
}

func (s *Server) ready(w http.ResponseWriter, _ *http.Request) {
	if s.Store.Len() == 0 {
		writeErr(w, http.StatusServiceUnavailable, "rates not loaded yet")
		return
	}
	w.WriteHeader(http.StatusOK)
}

type ratesResponse struct {
	Source        string                 `json:"source"`
	Attribution   string                 `json:"attribution"`
	Base          string                 `json:"base"`
	Date          string                 `json:"date"`
	RequestedDate string                 `json:"requested_date,omitempty"`
	Derived       bool                   `json:"derived"`
	Stale         bool                   `json:"stale,omitempty"`
	Rates         map[string]float64     `json:"rates"`
	OtherSources  map[string]otherSource `json:"other_sources,omitempty"`
}

func (s *Server) latest(w http.ResponseWriter, r *http.Request) {
	if !validFormat(w, r) {
		return
	}
	base, symbols, ok := parseBaseSymbols(w, r)
	if !ok {
		return
	}
	day, x, err := s.latestDay()
	if err != nil {
		s.storeErr(w, err)
		return
	}
	out, err := rates.Cross(day, base, symbols)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	if wantCSV(r) {
		writeCSV(w, "latest_"+base+"_"+day.Date+".csv", base, base != "EUR", dayRows(day.Date, base, out, x))
		return
	}
	others := x.others(base, out, true)
	writeJSON(w, ratesResponse{
		Source: Source, Attribution: attribution(others), Base: base, Date: day.Date,
		Derived: base != "EUR", Stale: s.isStale(day), Rates: out, OtherSources: others,
	})
}

type convertResponse struct {
	Source       string                 `json:"source"`
	Attribution  string                 `json:"attribution"`
	Date         string                 `json:"date"`
	From         string                 `json:"from"`
	To           string                 `json:"to"`
	Amount       float64                `json:"amount"`
	Rate         float64                `json:"rate"`
	Result       float64                `json:"result"`
	Derived      bool                   `json:"derived"`
	Stale        bool                   `json:"stale,omitempty"`
	OtherSources map[string]otherSource `json:"other_sources,omitempty"`
}

func (s *Server) convert(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to := code(q.Get("from")), code(q.Get("to"))
	if !validCode(from) || !validCode(to) {
		writeErr(w, http.StatusBadRequest, "from and to must be 3-letter currency codes")
		return
	}
	amount := 1.0
	if a := q.Get("amount"); a != "" {
		v, err := strconv.ParseFloat(a, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			writeErr(w, http.StatusBadRequest, "amount must be a number")
			return
		}
		amount = v
	}
	var (
		day ecb.Day
		x   extra
		err error
	)
	if d := q.Get("date"); d != "" {
		if !validDate(d) {
			writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
		day, x, err = s.dayOn(d)
	} else {
		day, x, err = s.latestDay()
	}
	if err != nil {
		s.storeErr(w, err)
		return
	}
	out, err := rates.Cross(day, from, []string{to})
	if err != nil {
		s.storeErr(w, err)
		return
	}
	rate := 1.0
	if from != to {
		rate = out[to]
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	others := x.others(from, out, true)
	writeJSON(w, convertResponse{
		Source: Source, Attribution: attribution(others), Date: day.Date, From: from, To: to,
		Amount: amount, Rate: rate, Result: round(amount*rate, 6),
		Derived: from != "EUR" && from != to, Stale: q.Get("date") == "" && s.isStale(day),
		OtherSources: others,
	})
}

type rangeResponse struct {
	Source       string                        `json:"source"`
	Attribution  string                        `json:"attribution"`
	Base         string                        `json:"base"`
	Start        string                        `json:"start"`
	End          string                        `json:"end"`
	Derived      bool                          `json:"derived"`
	Rates        map[string]map[string]float64 `json:"rates"`
	OtherSources map[string]otherSource        `json:"other_sources,omitempty"`
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	if !validFormat(w, r) {
		return
	}
	base, symbols, ok := parseBaseSymbols(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if d := q.Get("date"); d != "" {
		if !validDate(d) {
			writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
			return
		}
		day, x, err := s.dayOn(d)
		if err != nil {
			s.storeErr(w, err)
			return
		}
		out, err := rates.Cross(day, base, symbols)
		if err != nil {
			s.storeErr(w, err)
			return
		}
		w.Header().Set("Cache-Control", cacheFor(d, s.now()))
		if wantCSV(r) {
			writeCSV(w, "history_"+base+"_"+day.Date+".csv", base, base != "EUR", dayRows(day.Date, base, out, x))
			return
		}
		others := x.others(base, out, true)
		writeJSON(w, ratesResponse{
			Source: Source, Attribution: attribution(others), Base: base, Date: day.Date,
			RequestedDate: d, Derived: base != "EUR", Rates: out, OtherSources: others,
		})
		return
	}

	start, end := q.Get("start"), q.Get("end")
	if !validDate(start) || !validDate(end) {
		writeErr(w, http.StatusBadRequest, "pass date=YYYY-MM-DD, or start and end as YYYY-MM-DD")
		return
	}
	st, _ := time.Parse(time.DateOnly, start)
	en, _ := time.Parse(time.DateOnly, end)
	if en.Before(st) {
		writeErr(w, http.StatusBadRequest, "end is before start")
		return
	}
	if en.Sub(st) > MaxRangeDays*24*time.Hour {
		writeErr(w, http.StatusBadRequest, "range is limited to 366 days per request")
		return
	}
	days, err := s.daysIn(start, end)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	resp := rangeResponse{
		Source: Source, Attribution: Attribution, Base: base, Start: start, End: end,
		Derived: base != "EUR", Rates: make(map[string]map[string]float64, len(days)),
	}
	var rows []csvRow
	for _, d := range days {
		out, err := rates.Cross(d.Day, base, symbols)
		if errors.Is(err, rates.ErrUnknownCurrency) {
			continue // currency not quoted that day (e.g. before it was added)
		}
		resp.Rates[d.Date] = out
		resp.OtherSources = mergeOthers(resp.OtherSources, d.x.others(base, out, false))
		rows = append(rows, dayRows(d.Date, base, out, d.x)...)
	}
	resp.Attribution = attribution(resp.OtherSources)
	w.Header().Set("Cache-Control", cacheFor(end, s.now()))
	if wantCSV(r) {
		writeCSV(w, "history_"+base+"_"+start+"_"+end+".csv", base, resp.Derived, rows)
		return
	}
	writeJSON(w, resp)
}

type currency struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

func (s *Server) currencies(w http.ResponseWriter, _ *http.Request) {
	day, x, err := s.latestDay()
	if err != nil {
		s.storeErr(w, err)
		return
	}
	list := make([]currency, 0, len(day.Rates))
	for c := range day.Rates {
		src := Source
		if x.has(c) {
			src = ExtraSource
		}
		list = append(list, currency{Code: c, Name: Names[c], Source: src})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Code < list[j].Code })
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, map[string]any{"date": day.Date, "currencies": list})
}

func (s *Server) isStale(day ecb.Day) bool {
	t, err := time.Parse(time.DateOnly, day.Date)
	if err != nil {
		return true
	}
	return s.now().Sub(t) > StaleAfter
}

func (s *Server) storeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, rates.ErrNoData):
		writeErr(w, http.StatusServiceUnavailable, "rates not loaded yet, retry shortly")
	case errors.Is(err, rates.ErrBeforeFirstDate):
		writeErr(w, http.StatusNotFound, "no rates before 1999-01-04")
	case errors.Is(err, rates.ErrUnknownCurrency):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

// cacheFor lets CDNs keep past dates for a day; anything recent stays short.
func cacheFor(date string, now time.Time) string {
	if t, err := time.Parse(time.DateOnly, date); err == nil && now.Sub(t) > 7*24*time.Hour {
		return "public, max-age=86400"
	}
	return "public, max-age=60"
}

func parseBaseSymbols(w http.ResponseWriter, r *http.Request) (string, []string, bool) {
	q := r.URL.Query()
	base := "EUR"
	if b := q.Get("base"); b != "" {
		base = code(b)
	}
	if !validCode(base) {
		writeErr(w, http.StatusBadRequest, "base must be a 3-letter currency code")
		return "", nil, false
	}
	var symbols []string
	if s := q.Get("symbols"); s != "" {
		for _, c := range strings.Split(s, ",") {
			c = code(c)
			if !validCode(c) {
				writeErr(w, http.StatusBadRequest, "symbols must be comma-separated 3-letter codes")
				return "", nil, false
			}
			symbols = append(symbols, c)
		}
	}
	return base, symbols, true
}

func code(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func validCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

func validDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

func round(x float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(x*p) / p
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
