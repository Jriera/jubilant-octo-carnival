import { type RuntimeExtensions } from "@zuplo/runtime";

/**
 * Runs once when the gateway boots. Nothing to register yet; the MCP server
 * is a plain route (/mcp) using mcpServerHandler.
 * Docs: https://zuplo.com/docs/programmable-api/runtime-extensions
 */
export function runtimeInit(_runtime: RuntimeExtensions) {}
