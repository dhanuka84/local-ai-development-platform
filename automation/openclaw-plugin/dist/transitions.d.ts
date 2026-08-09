import type { ControllerState, WorkflowRun } from "./types.js";
export type MirrorDisposition = "running" | "waiting" | "succeeded" | "failed" | "cancelled";
export declare function dispositionFor(state: string): MirrorDisposition;
export declare function controllerState(workflow: WorkflowRun): ControllerState;
