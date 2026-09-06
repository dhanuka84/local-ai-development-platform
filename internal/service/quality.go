package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func (s *Service) ConfigureSourceRoots(roots []string) {
	s.sourceRoots = append([]string(nil), roots...)
}

func (s *Service) verifySources(ctx context.Context, manifest domain.SourceManifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	reader, ok := s.artifacts.(interface {
		Read(context.Context, string) ([]byte, error)
	})
	if !ok {
		return domain.ErrEvidenceUnavailable
	}
	for _, source := range manifest.Sources {
		if source.ArtifactSHA256 != "" {
			if _, err := reader.Read(ctx, source.ArtifactSHA256); err != nil {
				return domain.ErrEvidenceUnavailable
			}
		}
		if source.Kind != "repository" {
			continue
		}
		path, err := filepath.Abs(source.Reference)
		if err != nil {
			return fmt.Errorf("%w: source_unavailable", domain.ErrQualityBlocked)
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("%w: source_unavailable", domain.ErrQualityBlocked)
		}
		allowed := false
		for _, root := range s.sourceRoots {
			root, err = filepath.Abs(root)
			if err != nil {
				continue
			}
			root, err = filepath.EvalSymlinks(root)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(root, path)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: source_unavailable", domain.ErrQualityBlocked)
		}
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		output, err := exec.CommandContext(checkCtx, "git", "--no-optional-locks", "-c", "safe.directory="+path, "-C", path, "rev-parse", "--verify", "refs/heads/"+source.Branch+"^{commit}").Output()
		cancel()
		if err != nil {
			return fmt.Errorf("%w: source_unavailable", domain.ErrQualityBlocked)
		}
		current := strings.TrimSpace(string(output))
		if source.ApplicableThrough != "" {
			for _, bounds := range [][2]string{{source.Revision, current}, {current, source.ApplicableThrough}} {
				checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := exec.CommandContext(checkCtx, "git", "--no-optional-locks", "-c", "safe.directory="+path, "-C", path, "merge-base", "--is-ancestor", bounds[0], bounds[1]).Run()
				cancel()
				if err != nil {
					return fmt.Errorf("%w: source_changed", domain.ErrQualityBlocked)
				}
			}
		} else if current != source.Revision {
			return fmt.Errorf("%w: source_changed", domain.ErrQualityBlocked)
		}
	}
	return nil
}

// RefreshKnowledgeSources is an internal local-worker operation. It performs
// actual reads of every source; no MCP caller can supply a verification result.
func (s *Service) RefreshKnowledgeSources(ctx context.Context, id string) error {
	repository, ok := s.repository.(domain.KnowledgeQualityRepository)
	if !ok {
		return domain.ErrEvidenceUnavailable
	}
	quality, err := repository.KnowledgeQuality(ctx, id)
	if err != nil {
		return err
	}
	governance, err := s.governance()
	if err != nil {
		return err
	}
	report, err := governance.GetKnowledgeValidation(ctx, quality.ValidationID)
	if err != nil {
		return err
	}
	verifyErr := s.verifyValidationEvidence(ctx, report)
	reason := "source_unavailable"
	if errors.Is(verifyErr, domain.ErrEvidenceUnavailable) {
		reason = "evidence_unavailable"
	}
	if verifyErr != nil && strings.Contains(verifyErr.Error(), "source_changed") {
		reason = "source_changed"
	}
	if err := repository.RecordSourceCheck(ctx, id, quality.Version, quality.ValidationID, verifyErr == nil, reason); err != nil {
		return err
	}
	return verifyErr
}

func (s *Service) verifyValidationEvidence(ctx context.Context, report domain.KnowledgeValidation) error {
	if err := report.Check(); err != nil {
		return domain.ErrEvidenceUnavailable
	}
	if err := s.verifySources(ctx, report.SourceManifest); err != nil {
		return err
	}
	reader, ok := s.artifacts.(interface {
		Read(context.Context, string) ([]byte, error)
	})
	if !ok {
		return domain.ErrEvidenceUnavailable
	}
	digests := []string{report.ReportArtifact.SHA256, report.SourceManifestSHA256}
	for _, command := range report.Commands {
		digests = append(digests, command.OutputSHA256)
	}
	for _, digest := range digests {
		if _, err := reader.Read(ctx, digest); err != nil {
			return domain.ErrEvidenceUnavailable
		}
	}
	return nil
}

func (s *Service) QualityReviews(ctx context.Context, project string, limit int) ([]domain.KnowledgeQuality, error) {
	if _, err := s.AuthorizeProjectAction(ctx, project, "knowledge_candidate", "quality-review", "read", nil); err != nil {
		return nil, err
	}
	repository, ok := s.repository.(domain.KnowledgeQualityRepository)
	if !ok {
		return nil, domain.ErrEvidenceUnavailable
	}
	return repository.ListQualityReviews(ctx, project, limit)
}

func (s *Service) vectorProjectionCurrent(ctx context.Context, item domain.KnowledgeItem, hit domain.VectorHit) (bool, error) {
	repository, ok := s.repository.(domain.KnowledgeQualityRepository)
	if !ok {
		return true, nil
	} // Test adapters; production PostgreSQL implements this boundary.
	quality, err := repository.KnowledgeQuality(ctx, item.ID)
	if err != nil || !quality.Eligible || quality.Version != item.Version || quality.ProjectionVerifiedAt == nil {
		return false, err
	}
	manifests, ok := s.repository.(domain.ProjectionRepository)
	if !ok {
		return false, domain.ErrEvidenceUnavailable
	}
	manifest, err := manifests.KnowledgeProjection(ctx, item.ID)
	if err != nil {
		return false, err
	}
	identity, ok := s.embedder.(interface{ EmbeddingIdentity() (string, string, int) })
	if !ok {
		return false, domain.ErrQualityBlocked
	}
	provider, model, _ := identity.EmbeddingIdentity()
	return manifest.Provider == provider && manifest.Model == model && manifest.Version == item.Version && hit.ProjectionSHA256 == manifest.Digest(), nil
}
