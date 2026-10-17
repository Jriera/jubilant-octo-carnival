import { environment, ZuploContext, ZuploRequest } from "@zuplo/runtime";

/**
 * Last inbound policy on every route: proves to the origin (Railway) that the
 * call came through this gateway, and stops the customer's API key from being
 * forwarded to it.
 */
export default async function (request: ZuploRequest, context: ZuploContext) {
  const headers = new Headers(request.headers);
  headers.delete("authorization");
  headers.set("x-gateway-secret", environment.GATEWAY_SECRET ?? "");
  return new ZuploRequest(request, { headers });
}
