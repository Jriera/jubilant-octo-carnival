import { ZuploContext, ZuploRequest } from "@zuplo/runtime";

/**
 * Some MCP clients can't send headers, only a URL. Accept the API key as
 * ?apiKey=... on the MCP route and pass it on as a Bearer token, which the
 * tool routes' monetization policies read. Listing tools needs no key.
 */
export default async function (request: ZuploRequest, context: ZuploContext) {
  const url = new URL(request.url);
  const key = url.searchParams.get("apiKey");
  if (!key || request.headers.get("authorization")) {
    return request;
  }
  const headers = new Headers(request.headers);
  headers.set("authorization", `Bearer ${key}`);
  url.searchParams.delete("apiKey");
  return new ZuploRequest(url.toString(), { method: request.method, headers, body: request.body });
}
