package official

import (
	"fmt"
	"time"
)

// Selection says which table a rule picks for a date.
type Selection int

const (
	// SameDayOrEarlier: the table published on the date, or the last one before
	// it on days without a publication.
	SameDayOrEarlier Selection = iota
	// DayBefore: the last table published strictly before the date.
	DayBefore
	// Period: the monthly table whose period contains the date.
	Period
)

// Country describes one jurisdiction's rule for tax and invoicing.
type Country struct {
	Code        string // ISO 3166-1 alpha-2
	Name        string
	Local       string // currency the rate converts into
	Source      string
	SourceName  string
	Attribution string
	Rule        string
	LegalBasis  string
	Select      Selection
	// InvoiceDateFirst: when the invoice is issued before the tax point, the
	// invoice date replaces the tax point as the anchor (Poland, art. 31a(2)).
	InvoiceDateFirst bool
	Series           *Series
}

// Countries is the built-in rule table. Rules are a summary of the cited law
// for the general case; special regimes (customs, advance payments, reverse
// charge) can differ.
func Countries() []*Country {
	return []*Country{
		{
			Code: "PL", Name: "Poland", Local: "PLN", Source: "NBP", SourceName: "Narodowy Bank Polski",
			Attribution: "Source: Narodowy Bank Polski (NBP), table A average rates.",
			Rule: "NBP table A average rate from the last business day before the tax point. " +
				"If the invoice is issued before the tax point, the last business day before the invoice date.",
			LegalBasis:       "Art. 31a(1)-(2), Polish VAT Act (ustawa o podatku od towarów i usług)",
			Select:           DayBefore,
			InvoiceDateFirst: true,
		},
		{
			Code: "CZ", Name: "Czech Republic", Local: "CZK", Source: "CNB", SourceName: "Česká národní banka",
			Attribution: "Source: Česká národní banka (CNB), central bank exchange rate fixing.",
			Rule: "CNB exchange rate fixing valid on the day the obligation to declare tax arises " +
				"(taxable supply or payment, whichever is earlier). Days without a fixing use the last earlier one.",
			LegalBasis: "§ 4(5), Czech VAT Act (zákon č. 235/2004 Sb., o dani z přidané hodnoty)",
			Select:     SameDayOrEarlier,
		},
		{
			Code: "HU", Name: "Hungary", Local: "HUF", Source: "MNB", SourceName: "Magyar Nemzeti Bank",
			Attribution: "Source: Magyar Nemzeti Bank (MNB), official exchange rates.",
			Rule: "MNB official rate valid on the day of fulfilment. Days without a rate use the last earlier one. " +
				"Applies to taxpayers who chose MNB rates with NAV (form 18T201T); otherwise a bank's selling rate applies.",
			LegalBasis: "§ 80, Hungarian VAT Act (2007. évi CXXVII. törvény)",
			Select:     SameDayOrEarlier,
		},
		{
			Code: "GB", Name: "United Kingdom", Local: "GBP", Source: "HMRC", SourceName: "HM Revenue & Customs",
			Attribution: "Source: HM Revenue & Customs monthly exchange rates. Contains public sector information licensed under the Open Government Licence v3.0.",
			Rule:        "HMRC monthly exchange rate for the calendar month containing the time of supply.",
			LegalBasis:  "HMRC VAT Notice 700, section 7.6",
			Select:      Period,
		},
	}
}

// Result is the rate a rule picked for a date.
type Result struct {
	Anchor      string // date the rule was applied to
	Table       Table
	Quote       Quote
	Provisional bool // the table the rule wants may not be published yet
}

// Resolve applies the country's rule. date is the tax point; invoiceDate is
// optional.
func (c *Country) Resolve(currency, date, invoiceDate string) (Result, error) {
	anchor := date
	if c.InvoiceDateFirst && invoiceDate != "" && invoiceDate < date {
		anchor = invoiceDate
	}
	var (
		t   Table
		err error
	)
	switch c.Select {
	case DayBefore:
		t, err = c.Series.Before(anchor)
	case SameDayOrEarlier:
		t, err = c.Series.OnOrBefore(anchor)
	case Period:
		t, err = c.Series.Covering(anchor)
	}
	if err != nil {
		return Result{}, err
	}
	q, ok := t.Rates[currency]
	if !ok {
		return Result{}, fmt.Errorf("%w: %s on %s", ErrUnknownCurrency, currency, t.Date)
	}
	res := Result{Anchor: anchor, Table: t, Quote: q}

	// A daily rule is provisional when the weekday it wants is later than
	// anything loaded: that table may not be out yet (or loading is behind).
	// Past holidays are not provisional, because later tables prove the gap.
	if c.Select != Period {
		want := lastWeekday(anchor, c.Select == DayBefore)
		last, _ := c.Series.Last()
		res.Provisional = t.Date < want && want > last.Date
	}
	return res, nil
}

// lastWeekday is the latest Monday-Friday on (or, when strict, before) date.
func lastWeekday(date string, strict bool) string {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	if strict {
		d = d.AddDate(0, 0, -1)
	}
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, -1)
	}
	return d.Format(time.DateOnly)
}
