// Package contracts embeds and validates the platform's versioned contracts.
package contracts

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

//go:embed workflow/v1/*.json knowledge/v1/*.json context/v1/*.json
var Files embed.FS

func Validate(name string, data []byte) error {
	raw, err := Files.ReadFile(name)
	if err != nil {
		return err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if err := resolved.Validate(value); err != nil {
		return err
	}
	// The schema library treats format as an annotation. Enforce our UUID and
	// RFC3339 formats explicitly, including references used by these contracts.
	var definition map[string]any
	if err := json.Unmarshal(raw, &definition); err != nil {
		return err
	}
	return formats(definition, definition, value)
}

func formats(root, schema map[string]any, value any) error {
	if ref, ok := schema["$ref"].(string); ok && strings.HasPrefix(ref, "#/$defs/") {
		defs, _ := root["$defs"].(map[string]any)
		child, _ := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		if err := formats(root, child, value); err != nil {
			return err
		}
	}
	if text, ok := value.(string); ok {
		switch schema["format"] {
		case "uuid":
			if !regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`).MatchString(text) {
				return fmt.Errorf("invalid UUID")
			}
		case "date-time":
			if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
				return fmt.Errorf("invalid date-time: %w", err)
			}
		}
	}
	if object, ok := value.(map[string]any); ok {
		properties, _ := schema["properties"].(map[string]any)
		for key, entry := range object {
			if child, ok := properties[key].(map[string]any); ok {
				if err := formats(root, child, entry); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
			}
		}
	}
	if array, ok := value.([]any); ok {
		child, _ := schema["items"].(map[string]any)
		for _, entry := range array {
			if err := formats(root, child, entry); err != nil {
				return err
			}
		}
	}
	return nil
}

// Check requires a passing and failing fixture for every checked-in schema.
func Check() error {
	return fs.WalkDir(Files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".schema.json") {
			return nil
		}
		for _, kind := range []string{"valid", "invalid"} {
			fixture, err := Files.ReadFile(strings.TrimSuffix(path, ".schema.json") + "." + kind + ".json")
			if err != nil {
				return fmt.Errorf("%s fixture: %w", path, err)
			}
			err = Validate(path, fixture)
			if (err == nil) != (kind == "valid") {
				return fmt.Errorf("%s %s fixture: unexpected result: %v", path, kind, err)
			}
		}
		return nil
	})
}
