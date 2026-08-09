package workpacket

import "testing"

func validPatchPacket() Packet {
	return Packet{
		SchemaVersion:      SchemaVersion,
		ID:                 "packet-1",
		Goal:               "Update the parser and its tests",
		Workspace:          ".",
		BaseRevision:       "HEAD",
		Mode:               ModePatch,
		TaskClass:          TaskDevelopment,
		DataClassification: DataInternal,
		AllowedFiles:       []string{"internal/parser/**", "internal/parser_test.go"},
		ForbiddenFiles:     []string{"internal/parser/credentials.json"},
		Rollback:           []string{"Revert the candidate patch."},
		Checks:             []Check{{Name: "unit", Argv: []string{"go", "test", "./internal/parser"}, TimeoutSeconds: 60}},
		Limits:             Limits{MaxChangedFiles: 4, MaxDiffLines: 200, MaxPatchBytes: 100000},
	}
}

func TestEvaluateAllowsBoundedDevelopmentPatch(t *testing.T) {
	result := Evaluate(validPatchPacket())
	if !result.Allowed || result.Risk != "medium" || result.Route != "local_bounded_patch" {
		t.Fatalf("unexpected evaluation: %#v", result)
	}
	if !allowedPath("internal/parser/lexer.go", validPatchPacket().AllowedFiles, result.EffectiveForbiddenFiles) {
		t.Fatal("expected recursive allowed path to match")
	}
	if allowedPath(".env.local", []string{"**"}, result.EffectiveForbiddenFiles) {
		t.Fatal("default secret path must remain forbidden")
	}
}

func TestEvaluateRequiresApprovalForProtectedPatch(t *testing.T) {
	packet := validPatchPacket()
	packet.Categories = []string{"authorization"}
	result := Evaluate(packet)
	if result.Allowed || result.Risk != "high" || !result.RequiresApproval {
		t.Fatalf("protected patch should require approval: %#v", result)
	}
	packet.ApprovedBy = "security-owner"
	result = Evaluate(packet)
	if !result.Allowed {
		t.Fatalf("approved protected patch rejected: %#v", result)
	}
}

func TestEvaluateMaintenanceFailsClosed(t *testing.T) {
	packet := validPatchPacket()
	packet.TaskClass = TaskMaintenance
	packet.CloudReview = true
	packet.CloudProvider = "kimi"
	result := Evaluate(packet)
	if result.Allowed {
		t.Fatal("maintenance cloud review must be rejected")
	}
	packet.CloudReview = false
	packet.CloudProvider = ""
	packet.LocalOnly = true
	result = Evaluate(packet)
	if !result.Allowed || result.Route != "local_maintenance" {
		t.Fatalf("local maintenance packet rejected: %#v", result)
	}
}

func TestEvaluateCloudDisclosurePolicy(t *testing.T) {
	packet := validPatchPacket()
	packet.CloudReview = true
	packet.CloudProvider = "codex"
	packet.DataClassification = DataConfidential
	result := Evaluate(packet)
	if result.Allowed {
		t.Fatal("confidential cloud review without approval must be rejected")
	}
	packet.CloudApprovedBy = "product-owner"
	result = Evaluate(packet)
	if !result.Allowed || result.Route != "cloud_review_advisory" {
		t.Fatalf("approved confidential review rejected: %#v", result)
	}
	packet.DataClassification = DataRestricted
	packet.LocalOnly = true
	result = Evaluate(packet)
	if result.Allowed {
		t.Fatal("restricted cloud review must always be rejected")
	}
}

func TestEvaluateRejectsEscapingPatterns(t *testing.T) {
	packet := validPatchPacket()
	packet.AllowedFiles = []string{"../outside/**"}
	result := Evaluate(packet)
	if result.Allowed {
		t.Fatal("parent traversal pattern must be rejected")
	}
}
