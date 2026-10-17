package official

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const nbpJSON = `[{"table":"A","no":"199/A/NBP/2026","effectiveDate":"2026-10-14","rates":[{"currency":"dolar amerykański","code":"USD","mid":3.6512},{"currency":"forint (Węgry)","code":"HUF","mid":0.011883}]},
{"table":"A","no":"200/A/NBP/2026","effectiveDate":"2026-10-15","rates":[{"currency":"dolar amerykański","code":"USD","mid":3.6601},{"currency":"forint (Węgry)","code":"HUF","mid":0.011898}]}]`

func TestParseNBP(t *testing.T) {
	tb, err := ParseNBP(strings.NewReader(nbpJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(tb) != 2 || tb[1].ID != "200/A/NBP/2026" || tb[1].Date != "2026-10-15" || tb[1].Rates["USD"].PerUnit() != 3.6601 {
		t.Fatalf("%+v", tb)
	}
}

const cnbYear = `Date|1 AUD|1 EUR|100 HUF|1 USD
02.01.2026|13.797|24.355|6.143|20.912
05.01.2026|13.835|24.330|6.151|20.950
Date|1 AUD|1 EUR|100 HUF|1 USD|1 XDR
06.01.2026|13.840|24.320|6.160|20.900|28.5
`

func TestParseCNBYear(t *testing.T) {
	tb, err := ParseCNBYear(strings.NewReader(cnbYear))
	if err != nil {
		t.Fatal(err)
	}
	if len(tb) != 3 || tb[0].Date != "2026-01-02" || tb[2].ID != "3/2026" {
		t.Fatalf("%+v", tb)
	}
	huf := tb[0].Rates["HUF"]
	if huf.Units != 100 || huf.PerUnit() != 0.06143 || huf.Convert(1000) != 61.43 {
		t.Fatalf("HUF %+v", huf)
	}
	if _, ok := tb[2].Rates["XDR"]; !ok {
		t.Fatal("header change not applied")
	}
	if _, err := ParseCNBYear(strings.NewReader("Date|1 USD\n02.01.2026|20.9|1.0\n")); err == nil {
		t.Fatal("want error for column mismatch")
	}
}

const mnbSOAP = `<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><GetExchangeRatesResponse xmlns="http://www.mnb.hu/webservices/" xmlns:i="http://www.w3.org/2001/XMLSchema-instance"><GetExchangeRatesResult>&lt;MNBExchangeRates&gt;&lt;Day date="2026-10-15"&gt;&lt;Rate unit="1" curr="EUR"&gt;391,25&lt;/Rate&gt;&lt;Rate unit="100" curr="JPY"&gt;220,10&lt;/Rate&gt;&lt;/Day&gt;&lt;/MNBExchangeRates&gt;</GetExchangeRatesResult></GetExchangeRatesResponse></s:Body></s:Envelope>`

const mnbCurrencies = `<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><GetCurrenciesResponse xmlns="http://www.mnb.hu/webservices/"><GetCurrenciesResult>&lt;MNBCurrencies&gt;&lt;Currencies&gt;&lt;Curr&gt;HUF&lt;/Curr&gt;&lt;Curr&gt;EUR&lt;/Curr&gt;&lt;Curr&gt;JPY&lt;/Curr&gt;&lt;/Currencies&gt;&lt;/MNBCurrencies&gt;</GetCurrenciesResult></GetCurrenciesResponse></s:Body></s:Envelope>`

func TestMNBClient(t *testing.T) {
	var gotAction, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(strings.Builder)
		_, _ = fmt.Fprint(b, r.Header.Get("SOAPAction"))
		gotAction = b.String()
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		if strings.Contains(gotAction, "GetCurrencies") {
			_, _ = w.Write([]byte(mnbCurrencies))
			return
		}
		_, _ = w.Write([]byte(mnbSOAP))
	}))
	defer srv.Close()
	m := NewMNB(srv.URL)
	m.Now = func() time.Time { return time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC) }
	tb, err := m.Recent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotAction, "MNBArfolyamServiceSoap/GetExchangeRates") || !strings.Contains(gotBody, "<web:currencyNames>EUR,JPY</web:currencyNames>") {
		t.Fatalf("request: %s %s", gotAction, gotBody)
	}
	if len(tb) != 1 || tb[0].Rates["EUR"].Value != 391.25 || tb[0].Rates["JPY"].PerUnit() != 2.201 {
		t.Fatalf("%+v", tb)
	}
}

const hmrcXML = `<?xml version="1.0" encoding="UTF-8"?>
<exchangeRateMonthList Period="01/Oct/2026 to 31/Oct/2026">
  <exchangeRate><countryName>Euro</countryName><countryCode>EU</countryCode><currencyName>Euro</currencyName><currencyCode>EUR</currencyCode><rateNew>1.1500</rateNew></exchangeRate>
  <exchangeRate><countryName>USA</countryName><countryCode>US</countryCode><currencyName>Dollar</currencyName><currencyCode>USD</currencyCode><rateNew>1.3400</rateNew></exchangeRate>
</exchangeRateMonthList>`

func TestHMRCClient(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "monthly_xml_2026-10.xml") {
			_, _ = w.Write([]byte(hmrcXML))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	h := NewHMRC(srv.URL)
	h.Now = func() time.Time { return time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC) }
	tb, err := h.Recent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || !strings.HasSuffix(paths[1], "monthly_xml_2026-11.xml") {
		t.Fatalf("paths %v", paths)
	}
	if len(tb) != 1 || tb[0].Date != "2026-10-01" || tb[0].ValidTo != "2026-10-31" {
		t.Fatalf("%+v", tb)
	}
	usd := tb[0].Rates["USD"]
	if !usd.Inverse || usd.Convert(134) != 100 {
		t.Fatalf("USD %+v", usd)
	}
}

func TestNBPHistoryWindows(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		http.NotFound(w, r) // NBP answers 404 for windows with no tables
	}))
	defer srv.Close()
	n := NewNBP(srv.URL)
	n.Now = func() time.Time { return time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC) }
	if _, err := n.History(context.Background(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 || paths[0] != "/2026-01-01/2026-03-31/" || paths[1] != "/2026-04-01/2026-06-29/" || paths[2] != "/2026-06-30/2026-06-30/" {
		t.Fatalf("%v", paths)
	}
}

func series(tables ...Table) *Series {
	s := NewSeries()
	s.Merge(tables)
	return s
}

func q(v float64) map[string]Quote { return map[string]Quote{"USD": {Value: v, Units: 1}} }

func TestRules(t *testing.T) {
	cs := map[string]*Country{}
	for _, c := range Countries() {
		cs[c.Code] = c
	}
	daily := series(
		Table{Date: "2026-10-09", ID: "196", Rates: q(3.60)}, // Friday
		Table{Date: "2026-10-12", ID: "197", Rates: q(3.61)}, // Monday
		Table{Date: "2026-10-13", ID: "198", Rates: q(3.62)},
		Table{Date: "2026-10-15", ID: "199", Rates: q(3.64)}, // 14th: holiday
	)
	pl, cz := cs["PL"], cs["CZ"]
	pl.Series, cz.Series = daily, daily

	cases := []struct {
		c              *Country
		date, inv, tbl string
		prov           bool
	}{
		{pl, "2026-10-13", "", "197", false},           // day before
		{pl, "2026-10-12", "", "196", false},           // Monday -> Friday
		{pl, "2026-10-15", "", "198", false},           // holiday on 14th, later table exists
		{pl, "2026-10-15", "2026-10-13", "197", false}, // invoice issued first
		{pl, "2026-10-13", "2026-10-20", "197", false}, // later invoice is ignored
		{pl, "2026-10-17", "", "199", true},            // Saturday: Friday's table not loaded yet
		{cz, "2026-10-13", "", "198", false},           // same day
		{cz, "2026-10-14", "", "198", false},           // holiday -> previous
		{cz, "2026-10-16", "", "199", true},            // today's fixing not loaded yet
	}
	for _, tc := range cases {
		r, err := tc.c.Resolve("USD", tc.date, tc.inv)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.c.Code, tc.date, err)
		}
		if r.Table.ID != tc.tbl || r.Provisional != tc.prov {
			t.Errorf("%s %s inv=%s: table %s prov %v, want %s %v", tc.c.Code, tc.date, tc.inv, r.Table.ID, r.Provisional, tc.tbl, tc.prov)
		}
	}
	if _, err := pl.Resolve("USD", "2026-10-09", ""); !errors.Is(err, ErrNotPublished) {
		t.Fatalf("before first table: %v", err)
	}
	if _, err := pl.Resolve("EUR", "2026-10-13", ""); !errors.Is(err, ErrUnknownCurrency) {
		t.Fatalf("unknown currency: %v", err)
	}

	gb := cs["GB"]
	gb.Series = series(Table{Date: "2026-10-01", ValidTo: "2026-10-31", ID: "October 2026",
		Rates: map[string]Quote{"USD": {Value: 1.34, Units: 1, Inverse: true}}})
	if r, err := gb.Resolve("USD", "2026-10-31", ""); err != nil || r.Table.ID != "October 2026" {
		t.Fatalf("GB: %v %+v", err, r)
	}
	if _, err := gb.Resolve("USD", "2026-11-01", ""); !errors.Is(err, ErrNotPublished) {
		t.Fatalf("GB next month: %v", err)
	}
}
