import { zuploMonetizationPlugin } from "@zuplo/zudoku-plugin-monetization";
import type { ZudokuConfig } from "zudoku";

// Developer portal for Exchange Rates (sign-up, API keys, plans, docs).
// https://zuplo.com/docs/dev-portal/zudoku/configuration/overview
const config: ZudokuConfig = {
  site: { title: "Exchange Rates" },
  metadata: {
    title: "Exchange Rates API",
    description:
      "Official central bank exchange rates, tax and invoicing rates, time series and CSV export.",
  },
  navigation: [
    {
      type: "category",
      label: "Documentation",
      items: [
        { type: "doc", file: "introduction" },
        { type: "doc", file: "mcp" },
      ],
    },
    { type: "link", to: "/api", label: "API Reference" },
  ],
  redirects: [{ from: "/", to: "/introduction" }],
  apis: [{ type: "file", input: "../config/routes.oas.json", path: "api" }],
  authentication: {
    // TODO before launch: this is Zuplo's demo Auth0 tenant and does not work
    // on a custom domain. Replace with your own identity provider, see
    // https://zuplo.com/docs/dev-portal/zudoku/configuration/authentication
    type: "auth0",
    domain: "auth.zuplo.site",
    clientId: "f8I87rdsCRo4nU2FHf0fHVwA9P7xi7Ml",
    audience: "https://api.example.com/",
  },
  apiKeys: { enabled: true },
  plugins: [zuploMonetizationPlugin()],
};

export default config;
