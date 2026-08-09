import type { OpenClawPluginApi, OpenClawPluginToolContext } from "openclaw/plugin-sdk/plugin-entry";
import { HybridMCPClient } from "./mcp-client.js";
import { controllerState, dispositionFor } from "./transitions.js";
import type { WorkflowResponse, WorkflowRun } from "./types.js";

export type ControllerConfig = {
  mcpUrl: string;
  tokenEnv: string;
  controllerAgentIds: string[];
  requestTimeoutMs?: number;
};

type ManagedFlowRecord = ReturnType<ReturnType<OpenClawPluginApi["runtime"]["tasks"]["managedFlows"]["fromToolContext"]>["createManaged"]>;

export class WorkflowController {
  private readonly mcp: HybridMCPClient;

  constructor(
    private readonly api: OpenClawPluginApi,
    private readonly toolContext: OpenClawPluginToolContext,
    private readonly config: ControllerConfig,
  ) {
    authorizeController(toolContext, config.controllerAgentIds);
    const token = process.env[config.tokenEnv]?.trim();
    if (!token) throw new Error(`controller token environment variable ${config.tokenEnv} is not set`);
    this.mcp = new HybridMCPClient({ url: config.mcpUrl, token, timeoutMs: config.requestTimeoutMs ?? 30_000 });
  }

  async start(input: {
    projectId: string;
    request: string;
    idempotencyKey: string;
    risk?: string;
    dataClassification?: string;
    kind?: string;
  }, signal?: AbortSignal): Promise<{ flow: ManagedFlowRecord; workflow: WorkflowRun }> {
    const flows = this.flows();
    const created = flows.createManaged({
      controllerId: "hybrid-workflow-controller/v1",
      goal: input.request,
      status: "running",
      currentStep: "persist-workflow",
      stateJson: { projectId: input.projectId, triggerKey: input.idempotencyKey },
    });
    try {
      const response = await this.mcp.callWorkflowTool("workflow_run_create", {
        project_id: input.projectId,
        request: input.request,
        idempotency_key: input.idempotencyKey,
        openclaw_flow_id: created.flowId,
        risk: input.risk,
        data_classification: input.dataClassification,
        kind: input.kind,
      }, signal);
      if (response.workflow.openclaw_flow_id && response.workflow.openclaw_flow_id !== created.flowId) {
        throw new Error(`workflow ${response.workflow.id} is already linked to OpenClaw flow ${response.workflow.openclaw_flow_id}`);
      }
      return { flow: this.mirror(created, response.workflow), workflow: response.workflow };
    } catch (error) {
      flows.fail({
        flowId: created.flowId,
        expectedRevision: created.revision,
        blockedSummary: errorMessage(error),
        stateJson: { projectId: input.projectId, triggerKey: input.idempotencyKey },
      });
      throw error;
    }
  }

  async status(workflowId: string, flowId?: string, signal?: AbortSignal): Promise<Record<string, unknown>> {
    const response = await this.mcp.callWorkflowTool("workflow_run_get", { workflow_id: workflowId }, signal);
    const flow = flowId ? this.flows().get(flowId) : undefined;
    return { workflow: response.workflow, flow };
  }

  async transition(input: {
    workflowId: string;
    flowId: string;
    flowExpectedRevision: number;
    workflowExpectedVersion: number;
    eventType: string;
    idempotencyKey: string;
    evidence?: string;
    payload?: Record<string, unknown>;
  }, signal?: AbortSignal): Promise<Record<string, unknown>> {
    const flow = this.requireFlow(input.flowId, input.flowExpectedRevision);
    const response = await this.mcp.callWorkflowTool("workflow_run_transition", {
      workflow_id: input.workflowId,
      expected_version: input.workflowExpectedVersion,
      event_type: input.eventType,
      idempotency_key: input.idempotencyKey,
      evidence: input.evidence,
      payload: input.payload,
    }, signal);
    const mirrored = this.mirror(flow, response.workflow);
    return { workflow: response.workflow, event: response.event, flow: mirrored };
  }

  async cancel(input: {
    workflowId: string;
    flowId: string;
    flowExpectedRevision: number;
    workflowExpectedVersion: number;
    idempotencyKey: string;
  }, signal?: AbortSignal): Promise<Record<string, unknown>> {
    this.requireFlow(input.flowId, input.flowExpectedRevision);
    const response = await this.mcp.callWorkflowTool("workflow_run_transition", {
      workflow_id: input.workflowId,
      expected_version: input.workflowExpectedVersion,
      event_type: "CANCEL_REQUESTED",
      idempotency_key: input.idempotencyKey,
    }, signal);
    const cancelled = await this.flows().cancel({
      flowId: input.flowId,
      cfg: this.toolContext.getRuntimeConfig?.() ?? this.toolContext.runtimeConfig ?? this.toolContext.config ?? this.api.config,
    });
    return { workflow: response.workflow, event: response.event, flow: cancelled.flow, cancelled: cancelled.cancelled };
  }

  private flows() {
    if (!this.toolContext.sessionKey) throw new Error("managed workflow tools require an OpenClaw session");
    return this.api.runtime.tasks.managedFlows.fromToolContext(this.toolContext);
  }

  private requireFlow(flowId: string, expectedRevision: number): ManagedFlowRecord {
    const flow = this.flows().get(flowId);
    if (!flow || flow.syncMode !== "managed" || !flow.controllerId) throw new Error(`managed flow not found: ${flowId}`);
    if (flow.revision !== expectedRevision) {
      throw new Error(`OpenClaw flow revision conflict: expected ${expectedRevision}, current ${flow.revision}`);
    }
    return flow as ManagedFlowRecord;
  }

  private mirror(flow: ManagedFlowRecord, workflow: WorkflowRun): ManagedFlowRecord {
    const flows = this.flows();
    const stateJson = controllerState(workflow);
    const disposition = dispositionFor(workflow.state);
    let mutation;
    if (disposition === "waiting") {
      mutation = flows.setWaiting({
        flowId: flow.flowId,
        expectedRevision: flow.revision,
        currentStep: workflow.state,
        stateJson,
        waitJson: { kind: "workflow_gate", state: workflow.state, workflowId: workflow.id },
      });
    } else if (disposition === "succeeded") {
      mutation = flows.finish({ flowId: flow.flowId, expectedRevision: flow.revision, stateJson });
    } else if (disposition === "failed") {
      mutation = flows.fail({
        flowId: flow.flowId, expectedRevision: flow.revision, stateJson,
        blockedSummary: `authoritative workflow entered ${workflow.state}`,
      });
    } else if (disposition === "cancelled") {
      mutation = flows.requestCancel({ flowId: flow.flowId, expectedRevision: flow.revision });
    } else {
      mutation = flows.resume({
        flowId: flow.flowId,
        expectedRevision: flow.revision,
        status: "running",
        currentStep: workflow.state,
        stateJson,
      });
    }
    if (!mutation.applied) {
      throw new Error(`OpenClaw flow mirror failed: ${mutation.code}`);
    }
    return mutation.flow;
  }
}

function authorizeController(context: OpenClawPluginToolContext, allowedAgentIds: string[]): void {
  if (!context.agentId || !allowedAgentIds.includes(context.agentId)) {
    throw new Error(`agent ${context.agentId ?? "<unknown>"} is not an authorized workflow controller`);
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
