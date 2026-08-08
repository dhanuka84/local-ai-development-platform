package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type LocalStore struct {
	root string
}

func NewLocalStore(root string) *LocalStore {
	return &LocalStore{root: root}
}

func (s *LocalStore) Put(ctx context.Context, data []byte, mediaType string) (domain.Artifact, error) {
	if err := ctx.Err(); err != nil {
		return domain.Artifact{}, err
	}
	digest := sha256.Sum256(data)
	hexDigest := hex.EncodeToString(digest[:])
	dir := filepath.Join(s.root, hexDigest[:2], hexDigest[2:4])
	path := filepath.Join(dir, hexDigest)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return domain.Artifact{}, fmt.Errorf("create artifact directory: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return artifact(hexDigest, path, mediaType, int64(len(data))), nil
	} else if !os.IsNotExist(err) {
		return domain.Artifact{}, fmt.Errorf("inspect artifact: %w", err)
	}

	temporary, err := os.CreateTemp(dir, ".artifact-*")
	if err != nil {
		return domain.Artifact{}, fmt.Errorf("create artifact: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return domain.Artifact{}, fmt.Errorf("secure artifact: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return domain.Artifact{}, fmt.Errorf("write artifact: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return domain.Artifact{}, fmt.Errorf("sync artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return domain.Artifact{}, fmt.Errorf("close artifact: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		if _, statErr := os.Stat(path); statErr != nil {
			return domain.Artifact{}, fmt.Errorf("publish artifact: %w", err)
		}
	}
	return artifact(hexDigest, path, mediaType, int64(len(data))), nil
}

func artifact(digest, path, mediaType string, size int64) domain.Artifact {
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	return domain.Artifact{
		SHA256:    digest,
		URI:       "file://" + filepath.ToSlash(path),
		MediaType: mediaType,
		SizeBytes: size,
	}
}
