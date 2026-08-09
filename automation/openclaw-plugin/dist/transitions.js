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
export function dispositionFor(state) {
    if (waitingStates.has(state))
        return "waiting";
    if (successfulStates.has(state))
        return "succeeded";
    if (failedStates.has(state))
        return "failed";
    if (cancelledStates.has(state))
        return "cancelled";
    return "running";
}
export function controllerState(workflow) {
    return {
        workflowId: workflow.id,
        projectId: workflow.project_id,
        workflowState: workflow.state,
        workflowVersion: workflow.version,
    };
}
