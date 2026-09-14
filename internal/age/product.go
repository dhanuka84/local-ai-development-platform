package age

import (
	"context"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

// AGE stores derived topology and immutable version IDs. Queries must hydrate
// accepted current PostgreSQL heads; a stale AGE vertex cannot establish truth.
func (s *Store) ProjectProductRecord(ctx context.Context, record domain.ProductRecord, edges []domain.ProductRelation) error {
	if !record.Eligible() || record.SHA256 != record.Digest() {
		return domain.ErrQualityBlocked
	}
	return s.withAGE(ctx, func(tx pgx.Tx) error {
		merge := func(r domain.ProductRecord) error {
			return s.execCypher(ctx, tx, `MERGE (record:ProductRecord {id:$id}) SET record.project_id=$project,record.product_id=$product,record.kind=$kind,record.version=$version,record.sha256=$sha RETURN record`, map[string]any{"id": r.ID, "project": r.ProjectID, "product": r.ProductID, "kind": r.Kind, "version": r.Version, "sha": r.SHA256})
		}
		if err := merge(record); err != nil {
			return err
		}
		for _, edge := range edges {
			if edge.ProjectID != record.ProjectID || edge.ProductID != record.ProductID || !domain.ValidProductRelation(edge.Kind) {
				return domain.ErrForbidden
			}
			for _, id := range []string{edge.FromID, edge.ToID} {
				r, err := s.authority.GetProductRecord(ctx, record.ProjectID, id, true)
				if err != nil {
					return err
				}
				if err = merge(r); err != nil {
					return err
				}
			}
			if err := s.execCypher(ctx, tx, `MATCH (a:ProductRecord {id:$from}), (b:ProductRecord {id:$to}) MERGE (a)-[r:PRODUCT_RELATION {id:$id}]->(b) SET r.project_id=$project,r.kind=$kind RETURN r`, map[string]any{"from": edge.FromID, "to": edge.ToID, "id": edge.ID, "project": edge.ProjectID, "kind": edge.Kind}); err != nil {
				return err
			}
		}
		return nil
	})
}
