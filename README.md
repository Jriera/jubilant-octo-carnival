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
| `GET /v1/currencies` | Supported currency codes and names |
| `GET /healthz`, `GET /readyz` | Liveness, and readiness once rates are loaded |

Every response carries `source`, `attribution` and the publication `date`.
Rates for a base other than EUR are cross rates computed from the ECB's EUR
rates and are marked `"derived": true`. Full spec: [openapi.yaml](openapi.yaml).

## How it works

On start the server downloads the ECB's full history file once and keeps all
of it in memory (a few MB). It then polls the small daily file every 10
minutes and adds new publications, rejecting days with missing currencies or
moves over 25%. Requests never wait on the ECB, so each replica answers from
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

## Develop

```sh
go test -race ./...
go run ./cmd/server
docker build -t exchange-rates-api .
```

## Deploy (Fly.io)

One-time setup:

```sh
fly auth login
fly apps create exchange-rates-api   # pick another name if taken, and update fly.toml
fly secrets set RAPIDAPI_PROXY_SECRET=...   # from the Rapid provider dashboard
fly tokens create deploy -a exchange-rates-api
```

Add the deploy token as the `FLY_API_TOKEN` repository secret on GitHub.
After that, every push to `main` that passes CI deploys automatically.
