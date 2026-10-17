import {
  HttpProblems,
  MonetizationInboundPolicy,
  ZuploContext,
  ZuploRequest,
} from "@zuplo/runtime";

/**
 * CSV export is a paid feature, but it is a query parameter (format=csv) or
 * an Accept header on otherwise free routes, so it can't be gated per route.
 * Runs after a monetization policy.
 */
export default async function (request: ZuploRequest, context: ZuploContext) {
  const url = new URL(request.url);
  const format = url.searchParams.get("format")?.toLowerCase();
  const wantsCSV =
    format === "csv" ||
    (!format && (request.headers.get("accept") ?? "").startsWith("text/csv"));
  if (!wantsCSV) {
    return request;
  }
  const subscription = MonetizationInboundPolicy.getSubscriptionData(context);
  if (!subscription?.entitlements?.["csv_export"]?.hasAccess) {
    return HttpProblems.forbidden(request, context, {
      detail: "CSV export is not included in your plan. Upgrade in the developer portal.",
    });
  }
  return request;
}
