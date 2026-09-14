package domain

type ProductCodeBinding struct {
	Schema       string   `json:"schema"`
	RepositoryID string   `json:"repository_id"`
	Revision     string   `json:"revision"`
	File         string   `json:"file"`
	Symbols      []string `json:"symbols"`
}
type ExecutionCodeBridge struct {
	Record  ProductBinding     `json:"record"`
	Binding ProductCodeBinding `json:"binding"`
	Graph   CodeGraph          `json:"graph"`
}
type ExecutionLink struct {
	From           string `json:"from"`
	Relation       string `json:"relation"`
	To             string `json:"to"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}
