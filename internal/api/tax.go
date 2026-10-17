package api

import (
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jriera/jubilant-octo-carnival/internal/official"
)

const TaxDisclaimer = "Rate selected by the stated rule for the general case. " +
	"Special regimes can differ; confirm with your tax adviser."

type publishedQuote struct {
	Value float64 `json:"value"`
	Quote string  `json:"quote"` // e.g. "PLN per 1 USD", "USD per 1 GBP"
}

type taxRateResponse struct {
	Country       string         `json:"country"`
	LocalCurrency string         `json:"local_currency"`
	Currency      string         `json:"currency"`
	Date          string         `json:"date"`
	InvoiceDate   string         `json:"invoice_date,omitempty"`
	AppliedDate   string         `json:"applied_date"`
	Rate          float64        `json:"rate"`
	Published     publishedQuote `json:"published"`
	Amount        *float64       `json:"amount,omitempty"`
	Result        *float64       `json:"result,omitempty"`
	Source        string         `json:"source"`
	Table         string         `json:"table,omitempty"`
	RateDate      string         `json:"rate_date"`
	ValidTo       string         `json:"valid_to,omitempty"`
	Provisional   bool           `json:"provisional,omitempty"`
	Rule          string         `json:"rule"`
	LegalBasis    string         `json:"legal_basis"`
	Attribution   string         `json:"attribution"`
	Disclaimer    string         `json:"disclaimer"`
}

// taxRate answers "which official rate must I use for this document date?".
func (s *Server) taxRate(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c := s.taxCountry(q.Get("country"))
	if c == nil {
		writeErr(w, http.StatusBadRequest, "country must be one of "+strings.Join(s.taxCodes(), ", "))
		return
	}
	cur := code(q.Get("currency"))
	if !validCode(cur) {
		writeErr(w, http.StatusBadRequest, "currency must be a 3-letter currency code")
		return
	}
	if cur == c.Local {
		writeErr(w, http.StatusBadRequest, "currency is the local currency of "+c.Code)
		return
	}
	date := q.Get("date")
	if !validDate(date) {
		writeErr(w, http.StatusBadRequest, "date (the tax point, YYYY-MM-DD) is required")
		return
	}
	inv := q.Get("invoice_date")
	if inv != "" && !validDate(inv) {
		writeErr(w, http.StatusBadRequest, "invoice_date must be YYYY-MM-DD")
		return
	}
	var amount *float64
	if a := q.Get("amount"); a != "" {
		v, err := strconv.ParseFloat(a, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			writeErr(w, http.StatusBadRequest, "amount must be a number")
			return
		}
		amount = &v
	}

	res, err := c.Resolve(cur, date, inv)
	switch {
	case errors.Is(err, official.ErrNoData):
		writeErr(w, http.StatusServiceUnavailable, c.Source+" rates not loaded yet, retry shortly")
		return
	case errors.Is(err, official.ErrNotPublished):
		first, _ := c.Series.First()
		writeErr(w, http.StatusNotFound, "no "+c.Source+" table for that date (loaded from "+first.Date+")")
		return
	case errors.Is(err, official.ErrUnknownCurrency):
		writeErr(w, http.StatusNotFound, cur+" is not quoted by "+c.Source+" for that date")
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Only the date the rule applies to has to be past: a tax point after an
	// earlier invoice is normal. One day of slack covers callers ahead of UTC.
	if res.Anchor > s.now().UTC().AddDate(0, 0, 1).Format(time.DateOnly) {
		writeErr(w, http.StatusBadRequest, "date is in the future")
		return
	}

	pq := res.Quote
	pub := publishedQuote{Value: pq.Value, Quote: c.Local + " per " + trimFloat(pq.Units) + " " + cur}
	if pq.Inverse {
		pub.Quote = cur + " per 1 " + c.Local
	}
	resp := taxRateResponse{
		Country: c.Code, LocalCurrency: c.Local, Currency: cur, Date: date, InvoiceDate: inv,
		AppliedDate: res.Anchor, Rate: roundSig(pq.PerUnit(), 8), Published: pub,
		Source: c.Source, Table: res.Table.ID, RateDate: res.Table.Date, ValidTo: res.Table.ValidTo,
		Provisional: res.Provisional, Rule: c.Rule, LegalBasis: c.LegalBasis,
		Attribution: c.Attribution, Disclaimer: TaxDisclaimer,
	}
	if amount != nil {
		out := round(pq.Convert(*amount), 4)
		resp.Amount, resp.Result = amount, &out
	}
	if res.Provisional {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", cacheFor(date, s.now()))
	}
	writeJSON(w, resp)
}

type taxCountryInfo struct {
	Country       string   `json:"country"`
	Name          string   `json:"name"`
	LocalCurrency string   `json:"local_currency"`
	Source        string   `json:"source"`
	SourceName    string   `json:"source_name"`
	Rule          string   `json:"rule"`
	LegalBasis    string   `json:"legal_basis"`
	From          string   `json:"from,omitempty"`
	To            string   `json:"to,omitempty"`
	Currencies    []string `json:"currencies"`
	Loaded        bool     `json:"loaded"`
}

func (s *Server) taxCountries(w http.ResponseWriter, _ *http.Request) {
	out := make([]taxCountryInfo, 0, len(s.Tax))
	for _, c := range s.Tax {
		info := taxCountryInfo{
			Country: c.Code, Name: c.Name, LocalCurrency: c.Local, Source: c.Source,
			SourceName: c.SourceName, Rule: c.Rule, LegalBasis: c.LegalBasis, Currencies: []string{},
		}
		if first, ok := c.Series.First(); ok {
			last, _ := c.Series.Last()
			info.From, info.Loaded = first.Date, true
			info.To = last.Date
			if last.ValidTo != "" {
				info.To = last.ValidTo
			}
			for k := range last.Rates {
				info.Currencies = append(info.Currencies, k)
			}
			sort.Strings(info.Currencies)
		}
		out = append(out, info)
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, map[string]any{"countries": out, "disclaimer": TaxDisclaimer})
}

func (s *Server) taxCountry(cc string) *official.Country {
	cc = strings.ToUpper(strings.TrimSpace(cc))
	if cc == "UK" {
		cc = "GB"
	}
	for _, c := range s.Tax {
		if c.Code == cc {
			return c
		}
	}
	return nil
}

func (s *Server) taxCodes() []string {
	var out []string
	for _, c := range s.Tax {
		out = append(out, c.Code)
	}
	return out
}

func trimFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// roundSig rounds to n significant digits.
func roundSig(x float64, n int) float64 {
	if x == 0 {
		return 0
	}
	p := math.Pow(10, float64(n-1)-math.Floor(math.Log10(math.Abs(x))))
	return math.Round(x*p) / p
}
