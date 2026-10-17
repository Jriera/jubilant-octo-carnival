package api

import (
	"strings"
	"testing"

	"github.com/Jriera/jubilant-octo-carnival/internal/official"
)

func withNBP(s *Server) *Server {
	a, b := official.NewSeries(), official.NewSeries()
	a.Merge([]official.Table{{Date: "2026-10-14", Rates: map[string]official.Quote{"EUR": {Value: 4.25, Units: 1}, "USD": {Value: 3.65, Units: 1}}}})
	b.Merge([]official.Table{{Date: "2026-10-14", Rates: map[string]official.Quote{"AED": {Value: 0.995, Units: 1}}}})
	s.Extra = &official.Extra{A: a, B: b}
	return s
}

func TestExtraCurrencies(t *testing.T) {
	h := withNBP(testServer("")).Handler()
	_, body := get(t, h, "/v1/latest?symbols=USD,AED", nil)
	r := body["rates"].(map[string]any)
	if r["USD"] != 1.165 || r["AED"] != 4.27136 {
		t.Fatalf("ECB must win for USD; NBP adds AED: %v", r)
	}
	o := body["other_sources"].(map[string]any)
	if len(o) != 1 || o["AED"].(map[string]any)["table"] != "B" || o["AED"].(map[string]any)["date"] != "2026-10-14" {
		t.Fatalf("other_sources: %v", o)
	}
	if !strings.Contains(body["attribution"].(string), "Narodowy Bank Polski") {
		t.Fatalf("attribution: %v", body["attribution"])
	}
	// Without NBP currencies the response is unchanged.
	_, body = get(t, h, "/v1/latest?symbols=USD", nil)
	if _, ok := body["other_sources"]; ok || strings.Contains(body["attribution"].(string), "NBP") {
		t.Fatalf("%v", body)
	}
	// Convert from an NBP currency.
	_, body = get(t, h, "/v1/convert?from=AED&to=EUR&amount=100", nil)
	if body["result"] != 23.4117 || body["other_sources"] == nil {
		t.Fatalf("%v", body)
	}
	// CSV marks the source per row.
	rec := getRaw(t, h, "/v1/latest?symbols=USD,AED&format=csv", nil)
	if !strings.Contains(rec.Body.String(), "2026-10-16,EUR,AED,4.27136,false,NBP") || !strings.Contains(rec.Body.String(), "2026-10-16,EUR,USD,1.165,false,ECB") {
		t.Fatalf("%s", rec.Body.String())
	}
	// Currencies lists them with their source.
	_, body = get(t, h, "/v1/currencies", nil)
	found := false
	for _, c := range body["currencies"].([]any) {
		m := c.(map[string]any)
		if m["code"] == "AED" {
			found = m["source"] == "NBP" && m["name"] == "UAE dirham"
		}
	}
	if !found {
		t.Fatalf("%v", body["currencies"])
	}
}
