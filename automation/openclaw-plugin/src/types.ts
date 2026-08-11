export type WorkflowRun = {
  id: string;
  project_id: string;
  state: string;
  version: number;
  risk: string;
  data_classification: string;
  openclaw_flow_id?: string;
};

export type WorkflowEvent = {
  id: string;
  event_type: string;
  from_state: string;
  to_state: string;
  actor_principal_id: string;
  actor_role: string;
};

export type WorkflowResponse = {
  workflow: WorkflowRun;
  event?: WorkflowEvent;
};

export type WorkflowTask = {
  id: string;
  workflow_id: string;
  project_id: string;
  ordinal: number;
  task_key: string;
  state: string;
  route: string;
  execution_mode: "auto" | "manual";
  version: number;
  candidate_id?: string;
};

export type WorkflowTaskEvent = {
  id: string;
  task_id: string;
  event_type: string;
  from_state: string;
  to_state: string;
  provider?: string;
  model?: string;
};

export type WorkflowTaskResponse = {
  task: WorkflowTask;
  event?: WorkflowTaskEvent;
};

export type ControllerState = {
  workflowId: string;
  projectId: string;
  workflowState: string;
  workflowVersion: number;
};
