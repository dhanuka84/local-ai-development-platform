package milvus

import (
	"context"
	"fmt"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

type Store struct {
	client     *milvusclient.Client
	collection string
	dimension  int
}

func Open(ctx context.Context, address, database, apiKey, collection string, dimension int) (*Store, error) {
	client, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address: address,
		DBName:  database,
		APIKey:  apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to Milvus: %w", err)
	}
	return &Store{client: client, collection: collection, dimension: dimension}, nil
}

func (s *Store) EnsureCollection(ctx context.Context) error {
	has, err := s.client.HasCollection(ctx, milvusclient.NewHasCollectionOption(s.collection))
	if err != nil {
		return fmt.Errorf("check Milvus collection: %w", err)
	}
	if has {
		return nil
	}
	option := milvusclient.SimpleCreateCollectionOptions(s.collection, int64(s.dimension)).
		WithPKFieldName("id").
		WithVarcharPK(true, 64).
		WithAutoID(false).
		WithVectorFieldName("embedding").
		WithMetricType(entity.COSINE).
		WithDynamicSchema(true)
	if err := s.client.CreateCollection(ctx, option); err != nil {
		return fmt.Errorf("create Milvus collection: %w", err)
	}
	return nil
}

func (s *Store) Upsert(ctx context.Context, item domain.KnowledgeItem, embedding []float32) error {
	if len(embedding) != s.dimension {
		return fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(embedding), s.dimension)
	}
	_, err := s.client.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(s.collection).
		WithVarcharColumn("id", []string{item.ID}).
		WithVarcharColumn("project_id", []string{item.ProjectID}).
		WithVarcharColumn("document_type", []string{"knowledge"}).
		WithFloatVectorColumn("embedding", s.dimension, [][]float32{embedding}))
	if err != nil {
		return fmt.Errorf("upsert Milvus knowledge %s: %w", item.ID, err)
	}
	return nil
}

func (s *Store) Search(ctx context.Context, projectID string, embedding []float32, limit int) ([]domain.VectorHit, error) {
	if len(embedding) != s.dimension {
		return nil, fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(embedding), s.dimension)
	}
	sets, err := s.client.Search(ctx, milvusclient.NewSearchOption(
		s.collection, limit, []entity.Vector{entity.FloatVector(embedding)},
	).WithANNSField("embedding").WithFilter("project_id == {project_id} && document_type == 'knowledge'").WithTemplateParam("project_id", projectID))
	if err != nil {
		return nil, fmt.Errorf("search Milvus: %w", err)
	}
	if len(sets) == 0 {
		return []domain.VectorHit{}, nil
	}
	set := sets[0]
	if set.Err != nil {
		return nil, set.Err
	}
	result := make([]domain.VectorHit, 0, set.Len())
	for i := 0; i < set.Len(); i++ {
		id, err := set.IDs.GetAsString(i)
		if err != nil {
			return nil, fmt.Errorf("read Milvus result id: %w", err)
		}
		score := float32(0)
		if i < len(set.Scores) {
			score = set.Scores[i]
		}
		result = append(result, domain.VectorHit{ID: id, Score: score})
	}
	return result, nil
}

func (s *Store) UpsertRelation(ctx context.Context, relation domain.RepositoryRelation, embedding []float32) error {
	if len(embedding) != s.dimension {
		return fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(embedding), s.dimension)
	}
	_, err := s.client.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(s.collection).
		WithVarcharColumn("id", []string{relation.ID}).
		WithVarcharColumn("project_id", []string{relation.ProjectID}).
		WithVarcharColumn("document_type", []string{"repository_relation"}).
		WithFloatVectorColumn("embedding", s.dimension, [][]float32{embedding}))
	if err != nil {
		return fmt.Errorf("upsert Milvus repository relation %s: %w", relation.ID, err)
	}
	return nil
}

func (s *Store) SearchRelations(ctx context.Context, projectID string, embedding []float32, limit int) ([]domain.VectorHit, error) {
	if len(embedding) != s.dimension {
		return nil, fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(embedding), s.dimension)
	}
	sets, err := s.client.Search(ctx, milvusclient.NewSearchOption(
		s.collection, limit, []entity.Vector{entity.FloatVector(embedding)},
	).WithANNSField("embedding").WithFilter("project_id == {project_id} && document_type == 'repository_relation'").WithTemplateParam("project_id", projectID))
	if err != nil {
		return nil, fmt.Errorf("search Milvus repository relations: %w", err)
	}
	if len(sets) == 0 {
		return []domain.VectorHit{}, nil
	}
	set := sets[0]
	if set.Err != nil {
		return nil, set.Err
	}
	result := make([]domain.VectorHit, 0, set.Len())
	for i := 0; i < set.Len(); i++ {
		id, err := set.IDs.GetAsString(i)
		if err != nil {
			return nil, err
		}
		score := float32(0)
		if i < len(set.Scores) {
			score = set.Scores[i]
		}
		result = append(result, domain.VectorHit{ID: id, Score: score})
	}
	return result, nil
}

func (s *Store) UpsertCodeEntity(ctx context.Context, codeEntity domain.CodeEntity, embedding []float32) error {
	if len(embedding) != s.dimension {
		return fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(embedding), s.dimension)
	}
	_, err := s.client.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(s.collection).
		WithVarcharColumn("id", []string{codeEntity.ID}).
		WithVarcharColumn("project_id", []string{codeEntity.ProjectID}).
		WithVarcharColumn("repository_id", []string{codeEntity.RepositoryID}).
		WithVarcharColumn("document_type", []string{"code_entity"}).
		WithFloatVectorColumn("embedding", s.dimension, [][]float32{embedding}))
	if err != nil {
		return fmt.Errorf("upsert Milvus code entity %s: %w", codeEntity.ID, err)
	}
	return nil
}

func (s *Store) UpsertCodeEntities(ctx context.Context, codeEntities []domain.CodeEntity, embeddings [][]float32) error {
	if len(codeEntities) != len(embeddings) {
		return fmt.Errorf("code entity count %d does not match embedding count %d", len(codeEntities), len(embeddings))
	}
	if len(codeEntities) == 0 {
		return nil
	}
	ids := make([]string, len(codeEntities))
	projectIDs := make([]string, len(codeEntities))
	repositoryIDs := make([]string, len(codeEntities))
	documentTypes := make([]string, len(codeEntities))
	for index, codeEntity := range codeEntities {
		if len(embeddings[index]) != s.dimension {
			return fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(embeddings[index]), s.dimension)
		}
		ids[index] = codeEntity.ID
		projectIDs[index] = codeEntity.ProjectID
		repositoryIDs[index] = codeEntity.RepositoryID
		documentTypes[index] = "code_entity"
	}
	_, err := s.client.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(s.collection).
		WithVarcharColumn("id", ids).
		WithVarcharColumn("project_id", projectIDs).
		WithVarcharColumn("repository_id", repositoryIDs).
		WithVarcharColumn("document_type", documentTypes).
		WithFloatVectorColumn("embedding", s.dimension, embeddings))
	if err != nil {
		return fmt.Errorf("batch upsert %d Milvus code entities: %w", len(codeEntities), err)
	}
	return nil
}

func (s *Store) SearchCodeEntities(ctx context.Context, projectID, repositoryID string, embedding []float32, limit int) ([]domain.VectorHit, error) {
	if len(embedding) != s.dimension {
		return nil, fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(embedding), s.dimension)
	}
	option := milvusclient.NewSearchOption(
		s.collection, limit, []entity.Vector{entity.FloatVector(embedding)},
	).WithANNSField("embedding").WithFilter("project_id == {project_id} && document_type == 'code_entity'").WithTemplateParam("project_id", projectID)
	if repositoryID != "" {
		option = option.WithFilter("project_id == {project_id} && repository_id == {repository_id} && document_type == 'code_entity'").
			WithTemplateParam("repository_id", repositoryID)
	}
	sets, err := s.client.Search(ctx, option)
	if err != nil {
		return nil, fmt.Errorf("search Milvus code entities: %w", err)
	}
	if len(sets) == 0 {
		return []domain.VectorHit{}, nil
	}
	set := sets[0]
	if set.Err != nil {
		return nil, set.Err
	}
	result := make([]domain.VectorHit, 0, set.Len())
	for i := 0; i < set.Len(); i++ {
		id, err := set.IDs.GetAsString(i)
		if err != nil {
			return nil, err
		}
		score := float32(0)
		if i < len(set.Scores) {
			score = set.Scores[i]
		}
		result = append(result, domain.VectorHit{ID: id, Score: score})
	}
	return result, nil
}

func (s *Store) UpsertGraphEdge(ctx context.Context, edge domain.SemanticGraphEdge, embedding []float32) error {
	if len(embedding) != s.dimension {
		return fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(embedding), s.dimension)
	}
	_, err := s.client.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(s.collection).
		WithVarcharColumn("id", []string{edge.ID}).
		WithVarcharColumn("project_id", []string{edge.ProjectID}).
		WithVarcharColumn("repository_id", []string{edge.RepositoryID}).
		WithVarcharColumn("document_type", []string{"graph_edge"}).
		WithVarcharColumn("edge_type", []string{edge.Type}).
		WithFloatVectorColumn("embedding", s.dimension, [][]float32{embedding}))
	if err != nil {
		return fmt.Errorf("upsert Milvus graph edge %s: %w", edge.ID, err)
	}
	return nil
}

func (s *Store) SearchGraphEdges(ctx context.Context, projectID, repositoryID string, embedding []float32, limit int) ([]domain.VectorHit, error) {
	if len(embedding) != s.dimension {
		return nil, fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(embedding), s.dimension)
	}
	option := milvusclient.NewSearchOption(
		s.collection, limit, []entity.Vector{entity.FloatVector(embedding)},
	).WithANNSField("embedding").WithFilter("project_id == {project_id} && document_type == 'graph_edge'").WithTemplateParam("project_id", projectID)
	if repositoryID != "" {
		option = option.WithFilter("project_id == {project_id} && repository_id == {repository_id} && document_type == 'graph_edge'").
			WithTemplateParam("repository_id", repositoryID)
	}
	sets, err := s.client.Search(ctx, option)
	if err != nil {
		return nil, fmt.Errorf("search Milvus graph edges: %w", err)
	}
	if len(sets) == 0 {
		return []domain.VectorHit{}, nil
	}
	set := sets[0]
	if set.Err != nil {
		return nil, set.Err
	}
	result := make([]domain.VectorHit, 0, set.Len())
	for i := 0; i < set.Len(); i++ {
		id, err := set.IDs.GetAsString(i)
		if err != nil {
			return nil, err
		}
		score := float32(0)
		if i < len(set.Scores) {
			score = set.Scores[i]
		}
		result = append(result, domain.VectorHit{ID: id, Score: score})
	}
	return result, nil
}

func (s *Store) Ping(ctx context.Context) error {
	_, err := s.client.HasCollection(ctx, milvusclient.NewHasCollectionOption(s.collection))
	return err
}

func (s *Store) Close(ctx context.Context) error { return s.client.Close(ctx) }
