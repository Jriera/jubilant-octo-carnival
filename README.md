# Exchange Rates API

Fast, read-only API for official euro reference exchange rates published by
the European Central Bank, with history back to 1999.

Source: ECB statistics. Reference rates are for information purposes only.

## Endpoints

| Endpoint | Returns |
| --- | --- |
| `GET /v1/latest?base=EUR&symbols=USD,GBP` | Latest rates for a base currency |
| `GET /v1/convert?from=USD&to=CHF&amount=250` | Converted amount and the rate used (`date=` optional) |
| `GET /v1/history?date=2024-03-01` | Rates on a date (closest earlier publication) |
| `GET /v1/history?start=2024-01-01&end=2024-03-31` | Every publication in a range, up to 366 days |
| `GET /v1/timeseries?start=2024-01-01&end=2024-12-31&symbols=USD` | One series per currency with start, end, min, max and change, up to 10 years |
| `GET /v1/tax-rates?country=PL&currency=USD&date=2026-10-15` | Official rate the country's tax rule requires for that date (PL, CZ, HU, GB) |
| `GET /v1/tax-rates/countries` | Supported countries, rules, legal basis and loaded history |
| `GET /v1/currencies` | Supported currency codes and names |
| `GET /healthz`, `GET /readyz` | Liveness, and readiness once rates are loaded |

Every response carries `source`, `attribution` and the publication `date`.
Rates for a base other than EUR are cross rates computed from the ECB's EUR
rates and are marked `"derived": true`. Add `format=csv` (or send
`Accept: text/csv`) to `latest`, `history` or `timeseries` for a CSV download
with one rate per line. Full spec: [openapi.yaml](openapi.yaml).

## Tax and invoicing rates

`/v1/tax-rates` picks the official table a country's VAT rule points to and
returns the rate with the table number, publication date, rule and legal
basis, ready to print on an invoice or keep for an audit.

| Country | Source | Rule (general case) |
| --- | --- | --- |
| PL | NBP table A | Last business day before the tax point, or before the invoice date if the invoice is issued first (art. 31a VAT Act) |
| CZ | CNB fixing | Fixing valid on the day the tax obligation arises (§ 4(5) VAT Act) |
| HU | MNB | Rate valid on the day of fulfilment, for taxpayers who chose MNB rates (§ 80 VAT Act) |
| GB | HMRC monthly | Monthly rate for the month of the time of supply (VAT Notice 700, 7.6) |

Each source loads in the background (10 years of history; HMRC from 2021)
and is polled every 30 minutes. A source that fails to load answers 503 for its
country only. Special regimes (customs, advance payments, reverse charge) can
use other dates, so responses carry a disclaimer.

## How it works

On start the server downloads the ECB's full history file once and keeps all
of it in memory (a few MB). It then polls the small daily file every 10
minutes and adds new publications, skipping a day that looks truncated (more
than three currencies missing). Requests never wait on the ECB, so each replica answers from
memory and replicas need no shared database.

If the ECB has not published anything for more than four days, responses
carry `"stale": true`.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | Listen port |
| `RAPIDAPI_PROXY_SECRET` | unset | When set, `/v1` calls must send it in `X-RapidAPI-Proxy-Secret` |
| `REFRESH_INTERVAL` | `10m` | How often to poll the ECB daily file |
| `ECB_BASE_URL` | ECB site | Override the download location |
| `TAX_COUNTRIES` | `PL,CZ,HU,GB` | Tax-rate countries to load; `none` turns them off |
| `TAX_HISTORY_YEARS` | `10` | Years of tax-rate history to load |
| `TAX_REFRESH_INTERVAL` | `30m` | How often to poll the tax-rate sources |

## Develop

```sh
go test -race ./...
go run ./cmd/server
docker build -t exchange-rates-api .
```

## Deploy (Railway)

One-time setup in the Railway dashboard:

1. New Project, then "Deploy from GitHub repo", and pick this repository.
2. In the service settings, pick an EU region and turn on "Wait for CI" so only
   commits that pass CI are deployed.
3. Under Variables, add `RAPIDAPI_PROXY_SECRET` from the Rapid provider dashboard.
4. Under Networking, generate a public domain (or attach your own).

Railway builds the Dockerfile, sets `PORT`, and waits for `/readyz` before
switching traffic. After that, every merge to `main` deploys automatically.
