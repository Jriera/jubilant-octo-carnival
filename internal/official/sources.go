package official

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Fetcher downloads tables from one authority.
type Fetcher interface {
	// History loads every table from `from` up to today.
	History(ctx context.Context, from time.Time) ([]Table, error)
	// Recent loads the last few publications, for polling.
	Recent(ctx context.Context) ([]Table, error)
}

type httpClient struct {
	HTTP *http.Client
	Now  func() time.Time
}

func newHTTP() httpClient {
	return httpClient{HTTP: &http.Client{Timeout: 60 * time.Second}, Now: time.Now}
}

// errNotFound lets callers tell "nothing published" apart from a failure.
var errNotFound = fmt.Errorf("not found")

func (c httpClient) do(req *http.Request) ([]byte, error) {
	req.Header.Set("User-Agent", "exchange-rates-api/1.0")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s %s: status %d", req.Method, req.URL, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func (c httpClient) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// pause spaces out history requests so a cold start doesn't hammer a
// central bank's server.
func pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(250 * time.Millisecond):
		return nil
	}
}

// ---- NBP (Narodowy Bank Polski) mid rates, PLN per 1 unit ----
// Table A (~33 major currencies) is published every business day; table B
// (~115 other currencies) on Wednesdays.

const (
	NBPBaseURL  = "https://api.nbp.pl/api/exchangerates/tables/A"
	NBPBBaseURL = "https://api.nbp.pl/api/exchangerates/tables/B"
)

type NBP struct {
	httpClient
	BaseURL string
}

func NewNBP(baseURL string) *NBP {
	if baseURL == "" {
		baseURL = NBPBaseURL
	}
	return &NBP{httpClient: newHTTP(), BaseURL: baseURL}
}

// NewNBPB fetches table B.
func NewNBPB(baseURL string) *NBP {
	if baseURL == "" {
		baseURL = NBPBBaseURL
	}
	return NewNBP(baseURL)
}

// ParseNBP reads the JSON array the NBP API returns for tables A and B.
func ParseNBP(r io.Reader) ([]Table, error) {
	var raw []struct {
		No            string `json:"no"`
		EffectiveDate string `json:"effectiveDate"`
		Rates         []struct {
			Code string  `json:"code"`
			Mid  float64 `json:"mid"`
		} `json:"rates"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode nbp json: %w", err)
	}
	out := make([]Table, 0, len(raw))
	for _, t := range raw {
		if _, err := time.Parse(time.DateOnly, t.EffectiveDate); err != nil {
			return nil, fmt.Errorf("nbp: bad date %q", t.EffectiveDate)
		}
		tb := Table{Date: t.EffectiveDate, ID: t.No, Rates: map[string]Quote{}}
		for _, r := range t.Rates {
			if len(r.Code) != 3 || r.Mid <= 0 {
				return nil, fmt.Errorf("nbp: bad rate %s=%v on %s", r.Code, r.Mid, t.EffectiveDate)
			}
			tb.Rates[r.Code] = Quote{Value: r.Mid, Units: 1}
		}
		out = append(out, tb)
	}
	return out, nil
}

func (n *NBP) rangeTables(ctx context.Context, from, to time.Time) ([]Table, error) {
	url := fmt.Sprintf("%s/%s/%s/?format=json", n.BaseURL, from.Format(time.DateOnly), to.Format(time.DateOnly))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	body, err := n.do(req)
	if err == errNotFound {
		return nil, nil // no table in that window (holidays)
	}
	if err != nil {
		return nil, err
	}
	return ParseNBP(bytes.NewReader(body))
}

// History fetches in 90-day windows: the API refuses ranges over 93 days.
func (n *NBP) History(ctx context.Context, from time.Time) ([]Table, error) {
	now := n.Now()
	var out []Table
	for start := from; !start.After(now); start = start.AddDate(0, 0, 90) {
		end := start.AddDate(0, 0, 89)
		if end.After(now) {
			end = now
		}
		t, err := n.rangeTables(ctx, start, end)
		if err != nil {
			return nil, err
		}
		out = append(out, t...)
		if err := pause(ctx); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (n *NBP) Recent(ctx context.Context) ([]Table, error) {
	now := n.Now()
	return n.rangeTables(ctx, now.AddDate(0, 0, -14), now) // covers two table B weeks
}

// ---- CNB (Česká národní banka) fixing, CZK per Amount units ----

const CNBBaseURL = "https://www.cnb.cz/en/financial-markets/foreign-exchange-market/central-bank-exchange-rate-fixing/central-bank-exchange-rate-fixing"

type CNB struct {
	httpClient
	BaseURL string
}

func NewCNB(baseURL string) *CNB {
	if baseURL == "" {
		baseURL = CNBBaseURL
	}
	return &CNB{httpClient: newHTTP(), BaseURL: baseURL}
}

// ParseCNBYear reads year.txt: a "Date|1 AUD|100 HUF|..." header, then one
// "DD.MM.YYYY|rate|..." row per fixing. The header repeats whenever the set of
// currencies changes. Each fixing's number within the year (the "#N" the CNB
// prints on its daily table) is its position in the file.
func ParseCNBYear(r io.Reader) ([]Table, error) {
	type col struct {
		code  string
		units float64
	}
	var cols []col
	var out []Table
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Split(line, "|")
		if f[0] == "Date" {
			cols = cols[:0]
			for _, h := range f[1:] {
				parts := strings.Fields(h)
				if len(parts) != 2 {
					return nil, fmt.Errorf("cnb: bad header column %q", h)
				}
				u, err := strconv.ParseFloat(parts[0], 64)
				if err != nil || u <= 0 {
					return nil, fmt.Errorf("cnb: bad amount in %q", h)
				}
				cols = append(cols, col{parts[1], u})
			}
			continue
		}
		if cols == nil {
			return nil, fmt.Errorf("cnb: data before header")
		}
		d, err := time.Parse("02.01.2006", f[0])
		if err != nil {
			return nil, fmt.Errorf("cnb: bad date %q", f[0])
		}
		if len(f)-1 != len(cols) {
			return nil, fmt.Errorf("cnb: %s has %d rates, header has %d", f[0], len(f)-1, len(cols))
		}
		t := Table{Date: d.Format(time.DateOnly), Rates: map[string]Quote{}}
		for i, v := range f[1:] {
			v = strings.TrimSpace(strings.ReplaceAll(v, ",", "."))
			if v == "" {
				continue
			}
			x, err := strconv.ParseFloat(v, 64)
			if err != nil || x <= 0 {
				return nil, fmt.Errorf("cnb: bad rate %q for %s on %s", v, cols[i].code, f[0])
			}
			t.Rates[cols[i].code] = Quote{Value: x, Units: cols[i].units}
		}
		t.ID = fmt.Sprintf("%d/%d", len(out)+1, d.Year())
		out = append(out, t)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *CNB) year(ctx context.Context, y int) ([]Table, error) {
	body, err := c.get(ctx, fmt.Sprintf("%s/year.txt?year=%d", c.BaseURL, y))
	if err == errNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseCNBYear(bytes.NewReader(body))
}

func (c *CNB) History(ctx context.Context, from time.Time) ([]Table, error) {
	var out []Table
	for y := from.Year(); y <= c.Now().Year(); y++ {
		t, err := c.year(ctx, y)
		if err != nil {
			return nil, err
		}
		out = append(out, t...)
		if err := pause(ctx); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Recent re-reads the current year's file (about 30 KB by December). In the
// first days of January it also covers the end of the previous year.
func (c *CNB) Recent(ctx context.Context) ([]Table, error) {
	now := c.Now()
	out, err := c.year(ctx, now.Year())
	if err != nil {
		return nil, err
	}
	if now.YearDay() <= 7 {
		prev, err := c.year(ctx, now.Year()-1)
		if err != nil {
			return nil, err
		}
		out = append(prev, out...)
	}
	return out, nil
}

// ---- MNB (Magyar Nemzeti Bank) official rates, HUF per unit units ----

const MNBURL = "https://www.mnb.hu/arfolyamok.asmx"

type MNB struct {
	httpClient
	URL string
}

func NewMNB(url string) *MNB {
	if url == "" {
		url = MNBURL
	}
	return &MNB{httpClient: newHTTP(), URL: url}
}

// soapResult pulls the single *Result string out of a SOAP response. The MNB
// returns its data as an XML document escaped inside that string.
func soapResult(body []byte) ([]byte, error) {
	var env struct {
		Body struct {
			Inner struct {
				Items []struct {
					XMLName xml.Name
					Text    string `xml:",chardata"`
				} `xml:",any"`
			} `xml:",any"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode mnb soap: %w", err)
	}
	for _, it := range env.Body.Inner.Items {
		if strings.HasSuffix(it.XMLName.Local, "Result") {
			return []byte(it.Text), nil
		}
	}
	return nil, fmt.Errorf("mnb soap: no result element")
}

// ParseMNBRates reads <MNBExchangeRates><Day date=""><Rate unit="" curr="">314,89</Rate>.
func ParseMNBRates(doc []byte) ([]Table, error) {
	if len(bytes.TrimSpace(doc)) == 0 {
		return nil, nil
	}
	var raw struct {
		Days []struct {
			Date  string `xml:"date,attr"`
			Rates []struct {
				Unit string `xml:"unit,attr"`
				Curr string `xml:"curr,attr"`
				Text string `xml:",chardata"`
			} `xml:"Rate"`
		} `xml:"Day"`
	}
	if err := xml.Unmarshal(doc, &raw); err != nil {
		return nil, fmt.Errorf("decode mnb rates: %w", err)
	}
	var out []Table
	for _, d := range raw.Days {
		if _, err := time.Parse(time.DateOnly, d.Date); err != nil {
			return nil, fmt.Errorf("mnb: bad date %q", d.Date)
		}
		t := Table{Date: d.Date, Rates: map[string]Quote{}}
		for _, r := range d.Rates {
			v := strings.TrimSpace(strings.ReplaceAll(r.Text, ",", "."))
			if v == "" {
				continue
			}
			x, err := strconv.ParseFloat(v, 64)
			if err != nil || x <= 0 || len(r.Curr) != 3 {
				return nil, fmt.Errorf("mnb: bad rate %s=%q on %s", r.Curr, r.Text, d.Date)
			}
			u, err := strconv.ParseFloat(r.Unit, 64)
			if err != nil || u <= 0 {
				u = 1
			}
			t.Rates[r.Curr] = Quote{Value: x, Units: u}
		}
		out = append(out, t)
	}
	return out, nil
}

// ParseMNBCurrencies reads <MNBCurrencies><Currencies><Curr>HUF</Curr>...
func ParseMNBCurrencies(doc []byte) ([]string, error) {
	var raw struct {
		Curr []string `xml:"Currencies>Curr"`
	}
	if err := xml.Unmarshal(doc, &raw); err != nil {
		return nil, fmt.Errorf("decode mnb currencies: %w", err)
	}
	var out []string
	for _, c := range raw.Curr {
		if c = strings.TrimSpace(c); len(c) == 3 && c != "HUF" {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *MNB) call(ctx context.Context, op, params string) ([]byte, error) {
	env := `<?xml version="1.0" encoding="utf-8"?>` +
		`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:web="http://www.mnb.hu/webservices/">` +
		`<soap:Body><web:` + op + `>` + params + `</web:` + op + `></soap:Body></soap:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.URL, strings.NewReader(env))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", `"http://www.mnb.hu/webservices/MNBArfolyamServiceSoap/`+op+`"`)
	body, err := m.do(req)
	if err != nil {
		return nil, err
	}
	return soapResult(body)
}

func (m *MNB) rates(ctx context.Context, from, to time.Time, currencies []string) ([]Table, error) {
	doc, err := m.call(ctx, "GetExchangeRates",
		"<web:startDate>"+from.Format(time.DateOnly)+"</web:startDate>"+
			"<web:endDate>"+to.Format(time.DateOnly)+"</web:endDate>"+
			"<web:currencyNames>"+strings.Join(currencies, ",")+"</web:currencyNames>")
	if err != nil {
		return nil, err
	}
	return ParseMNBRates(doc)
}

func (m *MNB) currencies(ctx context.Context) ([]string, error) {
	doc, err := m.call(ctx, "GetCurrencies", "")
	if err != nil {
		return nil, err
	}
	return ParseMNBCurrencies(doc)
}

// History fetches one year per request.
func (m *MNB) History(ctx context.Context, from time.Time) ([]Table, error) {
	cur, err := m.currencies(ctx)
	if err != nil {
		return nil, err
	}
	now := m.Now()
	var out []Table
	for start := from; !start.After(now); start = start.AddDate(1, 0, 0) {
		end := start.AddDate(1, 0, -1)
		if end.After(now) {
			end = now
		}
		t, err := m.rates(ctx, start, end, cur)
		if err != nil {
			return nil, err
		}
		out = append(out, t...)
		if err := pause(ctx); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (m *MNB) Recent(ctx context.Context) ([]Table, error) {
	cur, err := m.currencies(ctx)
	if err != nil {
		return nil, err
	}
	now := m.Now()
	return m.rates(ctx, now.AddDate(0, 0, -10), now, cur)
}

// ---- HMRC monthly exchange rates, foreign currency per 1 GBP ----

const HMRCBaseURL = "https://www.trade-tariff.service.gov.uk/api/v2/exchange_rates/files"

// HMRCFirstMonth is the first month the trade tariff service publishes.
var HMRCFirstMonth = time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)

type HMRC struct {
	httpClient
	BaseURL string
}

func NewHMRC(baseURL string) *HMRC {
	if baseURL == "" {
		baseURL = HMRCBaseURL
	}
	return &HMRC{httpClient: newHTTP(), BaseURL: baseURL}
}

// ParseHMRC reads one monthly XML file. The period comes from the caller
// (the file's month) because the Period attribute's wording has changed over
// the years.
func ParseHMRC(r io.Reader, month time.Time) (Table, error) {
	var raw struct {
		Rates []struct {
			Code string `xml:"currencyCode"`
			Rate string `xml:"rateNew"`
		} `xml:"exchangeRate"`
	}
	if err := xml.NewDecoder(r).Decode(&raw); err != nil {
		return Table{}, fmt.Errorf("decode hmrc xml: %w", err)
	}
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	t := Table{
		Date:    first.Format(time.DateOnly),
		ValidTo: first.AddDate(0, 1, -1).Format(time.DateOnly),
		ID:      first.Format("January 2006"),
		Rates:   map[string]Quote{},
	}
	for _, x := range raw.Rates {
		code := strings.ToUpper(strings.TrimSpace(x.Code))
		v, err := strconv.ParseFloat(strings.TrimSpace(x.Rate), 64)
		if len(code) != 3 || err != nil || v <= 0 {
			continue // HMRC lists a few countries without a usable rate
		}
		// Several countries share a currency; their rates are identical.
		t.Rates[code] = Quote{Value: v, Units: 1, Inverse: true}
	}
	if len(t.Rates) == 0 {
		return Table{}, fmt.Errorf("hmrc %s: no rates", t.Date)
	}
	return t, nil
}

func (h *HMRC) month(ctx context.Context, m time.Time) (Table, bool, error) {
	// Month is not zero-padded in these file names (monthly_xml_2026-1.xml).
	body, err := h.get(ctx, fmt.Sprintf("%s/monthly_xml_%d-%d.xml", h.BaseURL, m.Year(), int(m.Month())))
	if err == errNotFound {
		return Table{}, false, nil
	}
	if err != nil {
		return Table{}, false, err
	}
	t, err := ParseHMRC(bytes.NewReader(body), m)
	return t, err == nil, err
}

// History loads every month from `from` (not before 2021) to next month,
// which HMRC publishes in the second half of the current month.
func (h *HMRC) History(ctx context.Context, from time.Time) ([]Table, error) {
	if from.Before(HMRCFirstMonth) {
		from = HMRCFirstMonth
	}
	now := h.Now()
	last := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	var out []Table
	for m := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); !m.After(last); m = m.AddDate(0, 1, 0) {
		t, ok, err := h.month(ctx, m)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, t)
		}
		if err := pause(ctx); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (h *HMRC) Recent(ctx context.Context) ([]Table, error) {
	now := h.Now()
	this := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var out []Table
	for _, m := range []time.Time{this, this.AddDate(0, 1, 0)} {
		t, ok, err := h.month(ctx, m)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, t)
		}
	}
	return out, nil
}
