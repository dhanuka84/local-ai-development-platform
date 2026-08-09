import type { OpenClawPluginApi, OpenClawPluginToolContext } from "openclaw/plugin-sdk/plugin-entry";
import type { WorkflowRun } from "./types.js";
export type ControllerConfig = {
    mcpUrl: string;
    tokenEnv: string;
    controllerAgentIds: string[];
    requestTimeoutMs?: number;
};
type ManagedFlowRecord = ReturnType<ReturnType<OpenClawPluginApi["runtime"]["tasks"]["managedFlows"]["fromToolContext"]>["createManaged"]>;
export declare class WorkflowController {
    private readonly api;
    private readonly toolContext;
    private readonly config;
    private readonly mcp;
    constructor(api: OpenClawPluginApi, toolContext: OpenClawPluginToolContext, config: ControllerConfig);
    start(input: {
        projectId: string;
        request: string;
        idempotencyKey: string;
        risk?: string;
        dataClassification?: string;
        kind?: string;
    }, signal?: AbortSignal): Promise<{
        flow: ManagedFlowRecord;
        workflow: WorkflowRun;
    }>;
    status(workflowId: string, flowId?: string, signal?: AbortSignal): Promise<Record<string, unknown>>;
    transition(input: {
        workflowId: string;
        flowId: string;
        flowExpectedRevision: number;
        workflowExpectedVersion: number;
        eventType: string;
        idempotencyKey: string;
        evidence?: string;
        payload?: Record<string, unknown>;
    }, signal?: AbortSignal): Promise<Record<string, unknown>>;
    cancel(input: {
        workflowId: string;
        flowId: string;
        flowExpectedRevision: number;
        workflowExpectedVersion: number;
        idempotencyKey: string;
    }, signal?: AbortSignal): Promise<Record<string, unknown>>;
    private flows;
    private requireFlow;
    private mirror;
}
export {};
