package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Jriera/jubilant-octo-carnival/internal/ecb"
	"github.com/Jriera/jubilant-octo-carnival/internal/rates"
)

func newServer(secret string) http.Handler {
	s := rates.New()
	s.Merge([]ecb.Day{
		{Date: "2026-10-15", Rates: map[string]float64{"USD": 1.16, "JPY": 174.8, "GBP": 0.865}},
		{Date: "2026-10-16", Rates: map[string]float64{"USD": 1.165, "JPY": 175.2, "GBP": 0.8674}},
	})
	now := func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }
	return (&Server{Store: s, ProxySecret: secret, Now: now}).Handler()
}

func get(t *testing.T, h http.Handler, url string, hdr map[string]string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestLatest(t *testing.T) {
	code, body := get(t, newServer(""), "/v1/latest", nil)
	if code != 200 {
		t.Fatalf("status %d: %v", code, body)
	}
	if body["date"] != "2026-10-16" || body["source"] != "ECB" || body["derived"] != false {
		t.Fatalf("body: %v", body)
	}
	if body["rates"].(map[string]any)["USD"] != 1.165 {
		t.Fatalf("rates: %v", body["rates"])
	}
}

func TestLatestCrossRate(t *testing.T) {
	code, body := get(t, newServer(""), "/v1/latest?base=usd&symbols=EUR,GBP", nil)
	if code != 200 || body["derived"] != true || body["base"] != "USD" {
		t.Fatalf("%d %v", code, body)
	}
	r := body["rates"].(map[string]any)
	if len(r) != 2 || r["GBP"] != 0.744549 {
		t.Fatalf("rates: %v", r)
	}
}

func TestConvert(t *testing.T) {
	code, body := get(t, newServer(""), "/v1/convert?from=EUR&to=USD&amount=100", nil)
	if code != 200 || body["result"] != 116.5 || body["derived"] != false {
		t.Fatalf("%d %v", code, body)
	}
	_, body = get(t, newServer(""), "/v1/convert?from=USD&to=EUR&amount=1", nil)
	if body["derived"] != true {
		t.Fatalf("USD->EUR should be derived: %v", body)
	}
}

func TestHistory(t *testing.T) {
	h := newServer("")
	// A Saturday returns Friday's rates and says so.
	code, body := get(t, h, "/v1/history?date=2026-10-17", nil)
	if code != 200 || body["date"] != "2026-10-16" || body["requested_date"] != "2026-10-17" {
		t.Fatalf("%d %v", code, body)
	}
	code, body = get(t, h, "/v1/history?start=2026-10-01&end=2026-10-31&symbols=USD", nil)
	if code != 200 || len(body["rates"].(map[string]any)) != 2 {
		t.Fatalf("%d %v", code, body)
	}
	if code, _ := get(t, h, "/v1/history?start=2020-01-01&end=2026-01-01", nil); code != 400 {
		t.Fatalf("long range: status %d", code)
	}
	if code, _ := get(t, h, "/v1/history?date=1990-01-01", nil); code != 404 {
		t.Fatalf("pre-1999: status %d", code)
	}
}

func TestBadInput(t *testing.T) {
	h := newServer("")
	for _, url := range []string{
		"/v1/latest?base=EURO",
		"/v1/latest?symbols=USD,XYZ",
		"/v1/convert?from=EUR",
		"/v1/convert?from=EUR&to=USD&amount=abc",
		"/v1/history",
		"/v1/history?date=17-10-2026",
	} {
		if code, _ := get(t, h, url, nil); code != 400 {
			t.Errorf("%s: status %d, want 400", url, code)
		}
	}
}

func TestCurrencies(t *testing.T) {
	code, body := get(t, newServer(""), "/v1/currencies", nil)
	list := body["currencies"].([]any)
	if code != 200 || len(list) != 4 {
		t.Fatalf("%d %v", code, body)
	}
	if first := list[0].(map[string]any); first["code"] != "EUR" || first["name"] != "Euro" {
		t.Fatalf("first: %v", first)
	}
}

func TestProxySecret(t *testing.T) {
	h := newServer("s3cret")
	if code, _ := get(t, h, "/v1/latest", nil); code != 403 {
		t.Fatalf("missing secret: %d", code)
	}
	if code, _ := get(t, h, "/v1/latest", map[string]string{"X-RapidAPI-Proxy-Secret": "s3cret"}); code != 200 {
		t.Fatalf("with secret: %d", code)
	}
	if code, _ := get(t, h, "/healthz", nil); code != 200 {
		t.Fatalf("healthz needs no secret: %d", code)
	}
}

func TestNotReadyAndStale(t *testing.T) {
	empty := (&Server{Store: rates.New()}).Handler()
	if code, _ := get(t, empty, "/readyz", nil); code != 503 {
		t.Fatalf("readyz: %d", code)
	}
	if code, _ := get(t, empty, "/v1/latest", nil); code != 503 {
		t.Fatalf("latest: %d", code)
	}

	s := rates.New()
	s.Merge([]ecb.Day{{Date: "2026-10-01", Rates: map[string]float64{"USD": 1.1}}})
	now := func() time.Time { return time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC) }
	_, body := get(t, (&Server{Store: s, Now: now}).Handler(), "/v1/latest", nil)
	if body["stale"] != true {
		t.Fatalf("expected stale: %v", body)
	}
}
