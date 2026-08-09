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

export type ControllerState = {
  workflowId: string;
  projectId: string;
  workflowState: string;
  workflowVersion: number;
};
