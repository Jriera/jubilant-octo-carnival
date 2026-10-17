package ecb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	f, err := os.Open("testdata/hist.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	days, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 {
		t.Fatalf("got %d days, want 3", len(days))
	}
	if days[0].Date != "2026-10-16" || days[0].Rates["USD"] != 1.165 {
		t.Fatalf("unexpected first day: %+v", days[0])
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"empty":     `<Envelope><Cube></Cube></Envelope>`,
		"bad rate":  `<Envelope><Cube><Cube time="2026-10-16"><Cube currency="USD" rate="x"/></Cube></Cube></Envelope>`,
		"zero rate": `<Envelope><Cube><Cube time="2026-10-16"><Cube currency="USD" rate="0"/></Cube></Cube></Envelope>`,
		"bad date":  `<Envelope><Cube><Cube time="16.10.2026"><Cube currency="USD" rate="1.1"/></Cube></Cube></Envelope>`,
		"not xml":   `{"rates":{}}`,
	}
	for name, doc := range cases {
		if _, err := Parse(strings.NewReader(doc)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != HistoryPath {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "testdata/hist.xml")
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if days, err := c.History(context.Background()); err != nil || len(days) != 3 {
		t.Fatalf("History: %v, %d days", err, len(days))
	}
	if _, err := c.Daily(context.Background()); err == nil {
		t.Fatal("Daily: expected error on 404")
	}
}
