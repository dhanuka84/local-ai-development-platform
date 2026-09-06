package milvus

import (
	"context"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

func (s *Store) UpsertDefinition(ctx context.Context, d domain.GovernedDefinition, embedding []float32) error {
	if d.Status != "approved" || d.Version < 1 || d.ProjectionModel == "" || d.ProjectionDimension != s.dimension {
		return domain.ErrQualityBlocked
	}
	if err := domain.ValidateEmbedding(embedding, s.dimension); err != nil {
		return err
	}
	_, err := s.client.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(s.collection).WithVarcharColumn("id", []string{d.VectorID()}).WithVarcharColumn("project_id", []string{d.ProjectID}).WithVarcharColumn("document_type", []string{"context_definition"}).WithInt64Column("version", []int64{int64(d.Version)}).WithVarcharColumn("content_sha256", []string{d.SHA256}).WithVarcharColumn("projection_sha256", []string{d.ProjectionDigest()}).WithFloatVectorColumn("embedding", s.dimension, [][]float32{embedding}))
	return err
}
func (s *Store) SearchDefinitions(ctx context.Context, project string, embedding []float32, limit int) ([]domain.VectorHit, error) {
	if err := domain.ValidateEmbedding(embedding, s.dimension); err != nil {
		return nil, err
	}
	sets, err := s.client.Search(ctx, milvusclient.NewSearchOption(s.collection, limit, []entity.Vector{entity.FloatVector(embedding)}).WithANNSField("embedding").WithFilter("project_id == {project} && document_type == 'context_definition'").WithTemplateParam("project", project).WithOutputFields("version", "content_sha256", "projection_sha256"))
	if err != nil {
		return nil, err
	}
	result := []domain.VectorHit{}
	for _, set := range sets {
		if set.Err != nil {
			return nil, set.Err
		}
		for i := 0; i < set.Len(); i++ {
			id, err := set.IDs.GetAsString(i)
			if err != nil {
				return nil, err
			}
			vc, dc, pc := set.GetColumn("version"), set.GetColumn("content_sha256"), set.GetColumn("projection_sha256")
			if vc == nil || dc == nil || pc == nil || i >= len(set.Scores) {
				continue
			}
			v, e1 := vc.GetAsInt64(i)
			d, e2 := dc.GetAsString(i)
			p, e3 := pc.GetAsString(i)
			if e1 != nil || e2 != nil || e3 != nil {
				continue
			}
			result = append(result, domain.VectorHit{ID: id, Version: int(v), ContentSHA256: d, ProjectionSHA256: p, Score: set.Scores[i]})
		}
	}
	return result, nil
}
func (s *Store) VerifyDefinitionProjection(ctx context.Context, d domain.GovernedDefinition) error {
	set, err := s.client.Query(ctx, milvusclient.NewQueryOption(s.collection).WithFilter("id == {id} && project_id == {project} && document_type == 'context_definition'").WithTemplateParam("id", d.VectorID()).WithTemplateParam("project", d.ProjectID).WithOutputFields("version", "content_sha256", "projection_sha256").WithConsistencyLevel(entity.ClStrong))
	if err != nil {
		return err
	}
	if set.ResultCount != 1 {
		return domain.ErrQualityBlocked
	}
	vc, dc, pc := set.GetColumn("version"), set.GetColumn("content_sha256"), set.GetColumn("projection_sha256")
	if vc == nil || dc == nil || pc == nil {
		return domain.ErrQualityBlocked
	}
	v, e1 := vc.GetAsInt64(0)
	digest, e2 := dc.GetAsString(0)
	projection, e3 := pc.GetAsString(0)
	if e1 != nil || e2 != nil || e3 != nil || int(v) != d.Version || digest != d.SHA256 || projection != d.ProjectionDigest() {
		return domain.ErrQualityBlocked
	}
	return nil
}
