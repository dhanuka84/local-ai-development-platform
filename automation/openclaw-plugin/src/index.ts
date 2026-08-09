import { defineToolPlugin } from "openclaw/plugin-sdk/tool-plugin";
import { jsonResult } from "openclaw/plugin-sdk/tool-results";
import { Type } from "typebox";
import { WorkflowController } from "./controller.js";

const configSchema = Type.Object({
  mcpUrl: Type.String({ description: "Loopback or private Streamable HTTP MCP endpoint." }),
  tokenEnv: Type.String({ description: "Environment variable containing the controller bearer token." }),
  controllerAgentIds: Type.Array(Type.String(), { minItems: 1, uniqueItems: true }),
  requestTimeoutMs: Type.Optional(Type.Integer({ minimum: 1000, maximum: 300000, default: 30000 })),
}, { additionalProperties: false });

const startParameters = Type.Object({
  projectId: Type.String(),
  request: Type.String(),
  idempotencyKey: Type.String(),
  kind: Type.Optional(Type.String()),
  risk: Type.Optional(Type.Union([Type.Literal("low"), Type.Literal("medium"), Type.Literal("high"), Type.Literal("critical")])),
  dataClassification: Type.Optional(Type.Union([Type.Literal("public"), Type.Literal("internal"), Type.Literal("confidential"), Type.Literal("restricted")])),
}, { additionalProperties: false });

const statusParameters = Type.Object({
  workflowId: Type.String(),
  flowId: Type.Optional(Type.String()),
}, { additionalProperties: false });

const transitionParameters = Type.Object({
  workflowId: Type.String(), flowId: Type.String(), flowExpectedRevision: Type.Integer({ minimum: 1 }),
  workflowExpectedVersion: Type.Integer({ minimum: 1 }), eventType: Type.String(), idempotencyKey: Type.String(),
  evidence: Type.Optional(Type.String()), payload: Type.Optional(Type.Record(Type.String(), Type.Unknown())),
}, { additionalProperties: false });

const cancelParameters = Type.Object({
  workflowId: Type.String(), flowId: Type.String(), flowExpectedRevision: Type.Integer({ minimum: 1 }),
  workflowExpectedVersion: Type.Integer({ minimum: 1 }), idempotencyKey: Type.String(),
}, { additionalProperties: false });

export default defineToolPlugin({
  id: "hybrid-workflow-controller",
  name: "Hybrid Workflow Controller",
  description: "Durably mirror OpenClaw managed Task Flows to the Hybrid AI PostgreSQL workflow authority.",
  configSchema,
  tools: (tool) => [
    tool({
      name: "hybrid_workflow_start",
      label: "Start Hybrid Workflow",
      description: "Create one managed Task Flow and one idempotent authoritative workflow.",
      parameters: startParameters,
      factory: ({ api, config, toolContext }) => ({
        name: "hybrid_workflow_start",
        label: "Start Hybrid Workflow",
        description: "Create one managed Task Flow and one idempotent authoritative workflow.",
        parameters: startParameters,
        execute: async (_toolCallId, params, signal) => jsonResult(await new WorkflowController(api, toolContext, config).start(params as never, signal)),
      }),
    }),
    tool({
      name: "hybrid_workflow_status",
      label: "Hybrid Workflow Status",
      description: "Read PostgreSQL workflow truth and its OpenClaw flow mirror.",
      parameters: statusParameters,
      factory: ({ api, config, toolContext }) => ({
        name: "hybrid_workflow_status",
        label: "Hybrid Workflow Status",
        description: "Read PostgreSQL workflow truth and its OpenClaw flow mirror.",
        parameters: statusParameters,
        execute: async (_toolCallId, params, signal) => {
          const input = params as { workflowId: string; flowId?: string };
          return jsonResult(await new WorkflowController(api, toolContext, config).status(input.workflowId, input.flowId, signal));
        },
      }),
    }),
    tool({
      name: "hybrid_workflow_transition",
      label: "Transition Hybrid Workflow",
      description: "Apply an authorized optimistic PostgreSQL transition and then mirror it to Task Flow.",
      parameters: transitionParameters,
      factory: ({ api, config, toolContext }) => ({
        name: "hybrid_workflow_transition",
        label: "Transition Hybrid Workflow",
        description: "Apply an authorized optimistic PostgreSQL transition and then mirror it to Task Flow.",
        parameters: transitionParameters,
        execute: async (_toolCallId, params, signal) => jsonResult(await new WorkflowController(api, toolContext, config).transition(params as never, signal)),
      }),
    }),
    tool({
      name: "hybrid_workflow_cancel",
      label: "Cancel Hybrid Workflow",
      description: "Persist sticky cancellation intent before cancelling OpenClaw child tasks.",
      parameters: cancelParameters,
      factory: ({ api, config, toolContext }) => ({
        name: "hybrid_workflow_cancel",
        label: "Cancel Hybrid Workflow",
        description: "Persist sticky cancellation intent before cancelling OpenClaw child tasks.",
        parameters: cancelParameters,
        execute: async (_toolCallId, params, signal) => jsonResult(await new WorkflowController(api, toolContext, config).cancel(params as never, signal)),
      }),
    }),
  ],
});
