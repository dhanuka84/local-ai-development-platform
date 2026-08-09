import { describe, expect, it } from "vitest";
import { controllerState, dispositionFor } from "./transitions.js";

describe("workflow mirror transitions", () => {
  it.each([
    ["intake", "running"],
    ["awaiting_human_qa", "waiting"],
    ["awaiting_product_approval", "waiting"],
    ["completed", "succeeded"],
    ["failed", "failed"],
    ["cancelled", "cancelled"],
  ])("maps %s to %s", (state, expected) => {
    expect(dispositionFor(state)).toBe(expected);
  });

  it("persists only correlation state in OpenClaw", () => {
    expect(controllerState({
      id: "workflow", project_id: "project", state: "ready", version: 4,
      risk: "low", data_classification: "internal",
    })).toEqual({
      workflowId: "workflow", projectId: "project", workflowState: "ready", workflowVersion: 4,
    });
  });
});
