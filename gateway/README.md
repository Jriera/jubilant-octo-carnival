# Exchange Rates gateway (Zuplo)

This folder is the [Zuplo](https://zuplo.com) project that sells the API
directly, next to the RapidAPI listing. Zuplo issues API keys, enforces plans,
bills through Stripe, hosts the developer portal and serves the MCP endpoint.
The Go service on Railway stays the origin; it only accepts calls that carry
`X-Gateway-Secret` (from here) or `X-RapidAPI-Proxy-Secret` (from RapidAPI).

```
customer / AI assistant ──► Zuplo (keys, plans, metering, /mcp) ──► Railway (Go API)
RapidAPI customer ────────► RapidAPI ─────────────────────────────► Railway (Go API)
```

## What is in here

| Path | What it does |
| --- | --- |
| `config/routes.oas.json` | Routes, generated from `../openapi.yaml` plus Zuplo settings. `/mcp` turns the GET routes into MCP tools. |
| `config/policies.json` | Plan checks: every route meters `api_requests`; `/v1/tax-rates` needs `tax_rates`, `/v1/timeseries` needs `timeseries`. |
| `modules/csv-gate.ts` | `format=csv` (or `Accept: text/csv`) needs `csv_export`. |
| `modules/upstream.ts` | Drops the customer key and adds `X-Gateway-Secret` before forwarding. |
| `modules/api-key-from-query.ts` | Lets MCP clients that can't set headers pass `?apiKey=`. |
| `docs/` | Developer portal (sign-up, keys, pricing, docs). |

When `openapi.yaml` gains an endpoint, add it to `config/routes.oas.json` too
(with an `operationId`, an `x-zuplo-route` and an MCP `operations` entry).
`go test ./...` fails if the two drift apart.

## One-time setup

1. **Zuplo project.** Create a project in the Zuplo Portal and connect it to
   this GitHub repo with `gateway` as the project folder, or create a Zuplo
   API key and add it as the `ZUPLO_API_KEY` secret in GitHub so
   `.github/workflows/deploy-gateway.yml` deploys on every push to `main`.
2. **Environment variables** (Zuplo Portal → Settings → Environment Variables):
   - `BACKEND_URL` = `https://jubilant-octo-carnival-production.up.railway.app`
   - `GATEWAY_SECRET` = a long random string, marked secret.
3. **Railway.** Set the same `GATEWAY_SECRET` on the service and redeploy.
   Keep `RAPIDAPI_PROXY_SECRET` as it is.
4. **Monetization** (Zuplo Portal → Services → Monetization):
   - Meter: `api_requests`.
   - Features (boolean): `tax_rates`, `timeseries`, `csv_export`.
   - Plans, a suggested start:

     | Plan | Price | Requests / month | tax_rates | timeseries | csv_export |
     | --- | --- | --- | --- | --- | --- |
     | Free | $0 | 1,000 | | | |
     | Starter | $9 | 20,000 | ✓ | ✓ | ✓ |
     | Pro | $29 | 100,000, then overage | ✓ | ✓ | ✓ |

   - Connect Stripe (test mode first). Zuplo needs to enable live billing on
     the account; ask at sales@zuplo.com.
5. **Developer portal sign-in.** `docs/zudoku.config.tsx` uses Zuplo's demo
   Auth0 tenant, which does not work on a custom domain. Replace it with your
   own provider (Auth0, Clerk, Supabase…) before launch.

## Run locally

Needs Node 24 or newer.

```bash
cd gateway
cp .env.example .env   # point BACKEND_URL at a local server if you like
npm install
npm run dev            # gateway on http://localhost:9000
npm run docs           # portal on http://localhost:9200 (pricing pages need `npx zuplo link` first)
```

## MCP

Clients connect to `https://<gateway-host>/mcp` (Streamable HTTP) with
`Authorization: Bearer <key>`, or `https://<gateway-host>/mcp?apiKey=<key>`
when they only take a URL. Each tool call is billed like the matching REST
call and is subject to the same plan features.
