import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import type { WorkflowResponse } from "./types.js";

export type MCPClientConfig = {
  url: string;
  token: string;
  timeoutMs: number;
};

type ToolResult = {
  structuredContent?: unknown;
  content?: Array<{ type: string; text?: string }>;
  isError?: boolean;
};

export class HybridMCPClient {
  constructor(private readonly config: MCPClientConfig) {}

  async callWorkflowTool(name: string, args: Record<string, unknown>, signal?: AbortSignal): Promise<WorkflowResponse> {
    const client = new Client({ name: "hybrid-openclaw-controller", version: "0.1.0" });
    const transport = new StreamableHTTPClientTransport(new URL(this.config.url), {
      requestInit: { headers: { Authorization: `Bearer ${this.config.token}` } },
    });
    const timeout = AbortSignal.timeout(this.config.timeoutMs);
    const combined = signal ? AbortSignal.any([signal, timeout]) : timeout;
    try {
      await client.connect(transport, { signal: combined });
      const result = (await client.callTool({ name, arguments: args }, undefined, {
        signal: combined,
      })) as ToolResult;
      if (result.isError) throw new Error(toolError(result));
      const value = result.structuredContent ?? parseTextContent(result);
      if (!isWorkflowResponse(value)) throw new Error(`${name} returned an invalid workflow response`);
      return value;
    } finally {
      await client.close().catch(() => undefined);
    }
  }
}

function parseTextContent(result: ToolResult): unknown {
  const text = result.content?.find((item) => item.type === "text")?.text;
  if (!text) return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

function toolError(result: ToolResult): string {
  return result.content?.find((item) => item.type === "text")?.text ?? "MCP tool call failed";
}

function isWorkflowResponse(value: unknown): value is WorkflowResponse {
  if (!value || typeof value !== "object") return false;
  const workflow = (value as { workflow?: unknown }).workflow;
  if (!workflow || typeof workflow !== "object") return false;
  const candidate = workflow as Record<string, unknown>;
  return typeof candidate.id === "string" && typeof candidate.project_id === "string" &&
    typeof candidate.state === "string" && typeof candidate.version === "number";
}
