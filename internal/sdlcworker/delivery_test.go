package sdlcworker

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
)

func TestStagingCompareAndSwapAndRestore(t *testing.T) {
	root := t.TempDir()
	v1 := []byte(`{"release":"one"}`)
	v2 := []byte(`{"release":"two"}`)
	if _, err := stageRelease(root, v1, []byte("archive one"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := stageRelease(root, v2, []byte("archive two"), ""); err == nil {
		t.Fatal("replacement lacked exact previous release")
	}
	if _, err := stageRelease(root, v2, []byte("archive two"), domain.Digest(v1)); err != nil {
		t.Fatal(err)
	}
	if _, err := stageRelease(root, v2, []byte("archive two"), domain.Digest(v1)); err != nil {
		t.Fatal("same action was not idempotent", err)
	}
	if _, err := stageRelease(root, v1, []byte("archive one"), domain.Digest(v2)); err != nil {
		t.Fatal("explicit restore failed", err)
	}
	current, err := os.ReadFile(filepath.Join(root, "current.json"))
	if err != nil || !bytes.Equal(current, v1) {
		t.Fatal("restore read-back failed")
	}
}

func TestEffectReceiptPreservesExactObservedBytes(t *testing.T) {
	raw := []byte("{\"value\": \"line\\n雪\"}\r\n")
	receipt := execution.EffectReceipt{Raw: string(raw), ObservedSHA256: domain.Digest(raw)}
	encoded, _ := json.Marshal(receipt)
	var decoded execution.EffectReceipt
	if json.Unmarshal(encoded, &decoded) != nil || domain.Digest([]byte(decoded.Raw)) != decoded.ObservedSHA256 {
		t.Fatal("JSON receipt changed native evidence bytes")
	}
}
