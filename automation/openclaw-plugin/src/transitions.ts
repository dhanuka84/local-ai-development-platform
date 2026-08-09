import type { ControllerState, WorkflowRun } from "./types.js";

const waitingStates = new Set([
  "needs_clarification",
  "awaiting_plan_approval",
  "awaiting_disclosure_approval",
  "awaiting_human_qa",
  "awaiting_product_approval",
  "cancel_requested",
  "blocked",
]);

const successfulStates = new Set(["completed"]);
const failedStates = new Set(["policy_rejected", "product_rejected", "failed"]);
const cancelledStates = new Set(["cancelled"]);

export type MirrorDisposition = "running" | "waiting" | "succeeded" | "failed" | "cancelled";

export function dispositionFor(state: string): MirrorDisposition {
  if (waitingStates.has(state)) return "waiting";
  if (successfulStates.has(state)) return "succeeded";
  if (failedStates.has(state)) return "failed";
  if (cancelledStates.has(state)) return "cancelled";
  return "running";
}

export function controllerState(workflow: WorkflowRun): ControllerState {
  return {
    workflowId: workflow.id,
    projectId: workflow.project_id,
    workflowState: workflow.state,
    workflowVersion: workflow.version,
  };
}
