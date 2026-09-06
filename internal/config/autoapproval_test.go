package config

import (
	"strings"
	"testing"
)

func TestAutomaticApprovalCannotBeEnabled(t *testing.T) {
	t.Setenv("AUTO_APPROVE_LOCAL", "true")
	_, err := LoadCLI()
	if err == nil || !strings.Contains(err.Error(), "AUTO_APPROVE_LOCAL") {
		t.Fatalf("automatic approval accepted: %v", err)
	}
}
