package age

import "testing"

func TestGraphNameValidation(t *testing.T) {
	valid := []string{"software_knowledge_graph", "graph1"}
	for _, value := range valid {
		if !graphNamePattern.MatchString(value) {
			t.Fatalf("valid graph name %q rejected", value)
		}
	}
	invalid := []string{"SoftwareGraph", "graph-name", "1graph", "graph'; DROP SCHEMA public; --"}
	for _, value := range invalid {
		if graphNamePattern.MatchString(value) {
			t.Fatalf("invalid graph name %q accepted", value)
		}
	}
}

func TestEveryAuthoritativeRelationTypeHasAnAGELabel(t *testing.T) {
	for _, relationType := range []string{
		"depends_on", "provides_api_to", "deploys_with", "shares_contract", "fork_of",
		"upstream_of", "successor_of", "contains", "related_to",
	} {
		if repositoryEdgeLabels[relationType] == "" {
			t.Fatalf("repository relation %q has no AGE label", relationType)
		}
	}
	for _, relationType := range []string{
		"contains", "defines", "imports", "calls", "references", "implements", "embeds", "tests",
	} {
		if codeEdgeLabels[relationType] == "" {
			t.Fatalf("code relation %q has no AGE label", relationType)
		}
	}
}
