package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStoreIsContentAddressed(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	first, err := store.Put(context.Background(), []byte("same"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(context.Background(), []byte("same"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 || first.URI != second.URI {
		t.Fatalf("same bytes produced different artifacts: %#v %#v", first, second)
	}
	path := strings.TrimPrefix(first.URI, "file://")
	if _, err := os.Stat(filepath.FromSlash(path)); err != nil {
		t.Fatalf("artifact was not stored: %v", err)
	}
}
