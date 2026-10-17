package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getRaw(t *testing.T, h http.Handler, url string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTimeseries(t *testing.T) {
	code, body := get(t, newServer(""), "/v1/timeseries?start=2026-10-01&end=2026-10-31&symbols=USD,JPY", nil)
	if code != 200 {
		t.Fatalf("%d %v", code, body)
	}
	usd := body["series"].(map[string]any)["USD"].([]any)
	if len(usd) != 2 || usd[0].(map[string]any)["date"] != "2026-10-15" || usd[1].(map[string]any)["rate"] != 1.165 {
		t.Fatalf("USD series: %v", usd)
	}
	st := body["stats"].(map[string]any)["USD"].(map[string]any)
	if st["start"] != 1.16 || st["end"] != 1.165 || st["min"] != 1.16 || st["max"] != 1.165 || st["change"] != 0.005 || st["change_percent"] != 0.431 {
		t.Fatalf("stats: %v", st)
	}
	if _, ok := body["series"].(map[string]any)["GBP"]; ok {
		t.Fatal("GBP not requested")
	}
}

func TestTimeseriesBadInput(t *testing.T) {
	h := newServer("")
	for _, url := range []string{
		"/v1/timeseries",
		"/v1/timeseries?start=2026-10-31&end=2026-10-01",
		"/v1/timeseries?start=2000-01-01&end=2026-01-01",
		"/v1/timeseries?start=2026-10-01&end=2026-10-31&symbols=XYZ",
		"/v1/timeseries?start=2026-10-01&end=2026-10-31&format=xml",
	} {
		if code, _ := get(t, h, url, nil); code != 400 {
			t.Errorf("%s: status %d, want 400", url, code)
		}
	}
}

func TestCSV(t *testing.T) {
	h := newServer("")
	rec := getRaw(t, h, "/v1/history?start=2026-10-01&end=2026-10-31&symbols=USD,GBP&format=csv", nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	want := "date,base,currency,rate,derived,source\n" +
		"2026-10-15,EUR,GBP,0.865,false,ECB\n" +
		"2026-10-15,EUR,USD,1.16,false,ECB\n" +
		"2026-10-16,EUR,GBP,0.8674,false,ECB\n" +
		"2026-10-16,EUR,USD,1.165,false,ECB\n"
	if rec.Body.String() != want {
		t.Fatalf("csv:\n%s", rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "history_EUR_2026-10-01_2026-10-31.csv") {
		t.Fatalf("disposition: %s", cd)
	}

	// Accept header works too, and other endpoints share the format.
	for _, url := range []string{"/v1/latest?symbols=USD", "/v1/history?date=2026-10-16&symbols=USD", "/v1/timeseries?start=2026-10-16&end=2026-10-16&symbols=USD"} {
		rec := getRaw(t, h, url, map[string]string{"Accept": "text/csv"})
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "2026-10-16,EUR,USD,1.165,false,ECB") {
			t.Errorf("%s: %d %q", url, rec.Code, rec.Body.String())
		}
	}
	// format=json wins over Accept.
	if rec := getRaw(t, h, "/v1/latest?format=json", map[string]string{"Accept": "text/csv"}); !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("format=json ignored")
	}
}
