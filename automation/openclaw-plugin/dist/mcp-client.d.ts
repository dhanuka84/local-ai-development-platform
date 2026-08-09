import type { WorkflowResponse } from "./types.js";
export type MCPClientConfig = {
    url: string;
    token: string;
    timeoutMs: number;
};
export declare class HybridMCPClient {
    private readonly config;
    constructor(config: MCPClientConfig);
    callWorkflowTool(name: string, args: Record<string, unknown>, signal?: AbortSignal): Promise<WorkflowResponse>;
}
