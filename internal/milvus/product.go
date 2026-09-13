package milvus

import (
	"context"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

func (s *Store) UpsertProductRecord(ctx context.Context, r domain.ProductRecord, p domain.ProductProjection, embedding []float32) error {
	if !r.Eligible() || r.SHA256 != r.Digest() || p.RecordID != r.ID || p.SHA256 != r.SHA256 || p.Model == "" || p.Dimension != s.dimension {
		return domain.ErrQualityBlocked
	}
	if err := domain.ValidateEmbedding(embedding, s.dimension); err != nil {
		return err
	}
	_, err := s.client.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(s.collection).
		WithVarcharColumn("id", []string{r.ID}).WithVarcharColumn("project_id", []string{r.ProjectID}).
		WithVarcharColumn("product_id", []string{r.ProductID}).WithVarcharColumn("document_type", []string{"product_record"}).
		WithInt64Column("version", []int64{int64(r.Version)}).WithVarcharColumn("content_sha256", []string{r.SHA256}).
		WithVarcharColumn("projection_sha256", []string{p.Digest()}).WithFloatVectorColumn("embedding", s.dimension, [][]float32{embedding}))
	return err
}

func (s *Store) SearchProductRecords(ctx context.Context, project, product string, embedding []float32, limit int) ([]domain.VectorHit, error) {
	if err := domain.ValidateEmbedding(embedding, s.dimension); err != nil {
		return nil, err
	}
	sets, err := s.client.Search(ctx, milvusclient.NewSearchOption(s.collection, limit, []entity.Vector{entity.FloatVector(embedding)}).WithANNSField("embedding").
		WithFilter("project_id == {project} && product_id == {product} && document_type == 'product_record'").WithTemplateParam("project", project).WithTemplateParam("product", product).
		WithOutputFields("version", "content_sha256", "projection_sha256"))
	if err != nil {
		return nil, err
	}
	out := []domain.VectorHit{}
	for _, set := range sets {
		if set.Err != nil {
			return nil, set.Err
		}
		for i := 0; i < set.Len(); i++ {
			id, e := set.IDs.GetAsString(i)
			if e != nil {
				return nil, e
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
			out = append(out, domain.VectorHit{ID: id, Version: int(v), ContentSHA256: d, ProjectionSHA256: p, Score: set.Scores[i]})
		}
	}
	return out, nil
}

func (s *Store) VerifyProductProjection(ctx context.Context, r domain.ProductRecord, p domain.ProductProjection) error {
	set, err := s.client.Query(ctx, milvusclient.NewQueryOption(s.collection).WithFilter("id == {id} && project_id == {project} && document_type == 'product_record'").
		WithTemplateParam("id", r.ID).WithTemplateParam("project", r.ProjectID).WithOutputFields("version", "content_sha256", "projection_sha256").WithConsistencyLevel(entity.ClStrong))
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
	d, e2 := dc.GetAsString(0)
	projection, e3 := pc.GetAsString(0)
	if e1 != nil || e2 != nil || e3 != nil || int(v) != r.Version || d != r.SHA256 || projection != p.Digest() {
		return domain.ErrQualityBlocked
	}
	return nil
}
