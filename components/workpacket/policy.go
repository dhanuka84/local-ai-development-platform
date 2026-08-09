package workpacket

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

var defaultForbiddenFiles = []string{
	".env",
	".env.*",
	"**/.env",
	"**/.env.*",
	".git/**",
	"**/.git/**",
}

var highRiskCategories = map[string]struct{}{
	"authentication": {},
	"authorization":  {},
	"cryptography":   {},
	"database":       {},
	"deployment":     {},
	"payment":        {},
	"permissions":    {},
	"production":     {},
	"secrets":        {},
}

func Evaluate(packet Packet) Evaluation {
	result := Evaluation{
		Risk:                    "low",
		Route:                   "local_readonly",
		EffectiveForbiddenFiles: mergeForbidden(packet.ForbiddenFiles),
	}
	packet.SchemaVersion = strings.TrimSpace(packet.SchemaVersion)
	packet.ID = strings.TrimSpace(packet.ID)
	packet.Goal = strings.TrimSpace(packet.Goal)
	packet.Workspace = strings.TrimSpace(packet.Workspace)
	packet.BaseRevision = strings.TrimSpace(packet.BaseRevision)
	packet.Mode = strings.ToLower(strings.TrimSpace(packet.Mode))
	packet.TaskClass = strings.ToLower(strings.TrimSpace(packet.TaskClass))
	packet.DataClassification = strings.ToLower(strings.TrimSpace(packet.DataClassification))
	packet.CloudProvider = strings.ToLower(strings.TrimSpace(packet.CloudProvider))
	packet.ApprovedBy = strings.TrimSpace(packet.ApprovedBy)
	packet.CloudApprovedBy = strings.TrimSpace(packet.CloudApprovedBy)

	if packet.SchemaVersion != SchemaVersion {
		result.Errors = append(result.Errors, fmt.Sprintf("schema_version must be %q", SchemaVersion))
	}
	if packet.ID == "" {
		result.Errors = append(result.Errors, "id is required")
	}
	if packet.Goal == "" {
		result.Errors = append(result.Errors, "goal is required")
	}
	if packet.Workspace == "" {
		result.Errors = append(result.Errors, "workspace is required")
	}
	if packet.Mode != ModeReadOnly && packet.Mode != ModePatch {
		result.Errors = append(result.Errors, "mode must be readonly or patch")
	}
	if packet.TaskClass != TaskDevelopment && packet.TaskClass != TaskMaintenance {
		result.Errors = append(result.Errors, "task_class must be development or maintenance")
	}
	if !slices.Contains([]string{DataPublic, DataInternal, DataConfidential, DataRestricted}, packet.DataClassification) {
		result.Errors = append(result.Errors, "data_classification must be public, internal, confidential, or restricted")
	}

	if packet.Mode == ModePatch {
		result.Risk = "medium"
		result.Route = "local_bounded_patch"
		if packet.BaseRevision == "" {
			result.Errors = append(result.Errors, "base_revision is required for patch mode")
		}
		if len(packet.AllowedFiles) == 0 {
			result.Errors = append(result.Errors, "allowed_files is required for patch mode")
		}
		if len(packet.Checks) == 0 {
			result.Errors = append(result.Errors, "at least one deterministic check is required for patch mode")
		}
		if len(cleanStrings(packet.Rollback)) == 0 {
			result.Errors = append(result.Errors, "rollback steps are required for patch mode")
		}
		if packet.Limits.MaxChangedFiles < 1 || packet.Limits.MaxDiffLines < 1 {
			result.Errors = append(result.Errors, "positive max_changed_files and max_diff_lines are required for patch mode")
		}
		if packet.Limits.MaxPatchBytes < 0 {
			result.Errors = append(result.Errors, "max_patch_bytes cannot be negative")
		}
	}

	for _, pattern := range append(append([]string{}, packet.AllowedFiles...), result.EffectiveForbiddenFiles...) {
		if err := validatePattern(pattern); err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
	}
	for i, check := range packet.Checks {
		if strings.TrimSpace(check.Name) == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("checks[%d].name is required", i))
		}
		if len(check.Argv) == 0 || strings.TrimSpace(check.Argv[0]) == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("checks[%d].argv must name an executable", i))
		}
		if check.TimeoutSeconds < 0 || check.TimeoutSeconds > 1800 {
			result.Errors = append(result.Errors, fmt.Sprintf("checks[%d].timeout_seconds must be between 0 and 1800", i))
		}
	}

	for _, category := range cleanStrings(packet.Categories) {
		if _, found := highRiskCategories[strings.ToLower(category)]; found {
			result.Risk = "high"
			result.RequiresApproval = packet.Mode == ModePatch
		}
	}
	if packet.DataClassification == DataConfidential {
		result.Risk = "high"
	}
	if packet.Destructive {
		result.Risk = "high"
		result.RequiresApproval = true
	}
	if result.RequiresApproval && packet.ApprovedBy == "" {
		result.Errors = append(result.Errors, "approved_by is required for high-risk or destructive patch execution")
	}

	if packet.TaskClass == TaskMaintenance {
		result.Route = "local_maintenance"
		if !packet.LocalOnly {
			result.Errors = append(result.Errors, "maintenance packets must set local_only=true")
		}
		if packet.CloudReview {
			result.Errors = append(result.Errors, "maintenance packets cannot request cloud review")
		}
	}
	if packet.LocalOnly && packet.CloudReview {
		result.Errors = append(result.Errors, "local_only and cloud_review cannot both be true")
	}
	if packet.CloudReview {
		result.Route = "cloud_review_advisory"
		if packet.CloudProvider != "codex" && packet.CloudProvider != "kimi" {
			result.Errors = append(result.Errors, "cloud_provider must be codex or kimi")
		}
		if packet.DataClassification == DataRestricted {
			result.Errors = append(result.Errors, "restricted data can never be sent to a cloud reviewer")
		}
		if packet.DataClassification == DataConfidential && packet.CloudApprovedBy == "" {
			result.Errors = append(result.Errors, "confidential cloud review requires cloud_approved_by")
		}
	}
	if packet.DataClassification == DataRestricted && !packet.LocalOnly {
		result.Errors = append(result.Errors, "restricted packets must set local_only=true")
	}
	if packet.LocalOnly {
		result.Warnings = append(result.Warnings, "local_only controls model routing; use an egress-denied process or container when hard offline enforcement is required")
	}
	result.Allowed = len(result.Errors) == 0
	return result
}

func mergeForbidden(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(defaultForbiddenFiles)+len(values))
	for _, value := range append(append([]string{}, defaultForbiddenFiles...), values...) {
		value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
		if value == "" {
			continue
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func validatePattern(pattern string) error {
	pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
	if pattern == "" {
		return fmt.Errorf("file pattern cannot be empty")
	}
	if path.IsAbs(pattern) || strings.HasPrefix(pattern, "/") {
		return fmt.Errorf("file pattern %q must be repository-relative", pattern)
	}
	for _, part := range strings.Split(pattern, "/") {
		if part == ".." {
			return fmt.Errorf("file pattern %q cannot traverse outside the repository", pattern)
		}
	}
	return nil
}

func allowedPath(name string, allowed, forbidden []string) bool {
	name = strings.TrimPrefix(strings.ReplaceAll(path.Clean(name), "\\", "/"), "./")
	for _, pattern := range forbidden {
		if globMatch(pattern, name) {
			return false
		}
	}
	for _, pattern := range allowed {
		if globMatch(pattern, name) {
			return true
		}
	}
	return false
}

func globMatch(pattern, name string) bool {
	pattern = strings.TrimPrefix(strings.ReplaceAll(path.Clean(pattern), "\\", "/"), "./")
	var expression strings.Builder
	expression.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					expression.WriteString("(?:.*/)?")
				} else {
					expression.WriteString(".*")
				}
			} else {
				expression.WriteString("[^/]*")
			}
		case '?':
			expression.WriteString("[^/]")
		default:
			expression.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	expression.WriteString("$")
	matched, err := regexp.MatchString(expression.String(), name)
	return err == nil && matched
}
