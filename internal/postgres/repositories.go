package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

const relationColumns = `r.id::text,r.project_id,
    f.id::text,f.project_id,f.name,f.canonical_url,f.default_branch,f.revision,f.created_at,f.updated_at,
    t.id::text,t.project_id,t.name,t.canonical_url,t.default_branch,t.revision,t.created_at,t.updated_at,
    r.relation_type,r.evidence,r.confidence,r.approved_by,r.created_at,r.updated_at`

func scanRelation(row pgx.Row) (domain.RepositoryRelation, error) {
	var relation domain.RepositoryRelation
	err := row.Scan(&relation.ID, &relation.ProjectID,
		&relation.From.ID, &relation.From.ProjectID, &relation.From.Name, &relation.From.CanonicalURL,
		&relation.From.DefaultBranch, &relation.From.Revision, &relation.From.CreatedAt, &relation.From.UpdatedAt,
		&relation.To.ID, &relation.To.ProjectID, &relation.To.Name, &relation.To.CanonicalURL,
		&relation.To.DefaultBranch, &relation.To.Revision, &relation.To.CreatedAt, &relation.To.UpdatedAt,
		&relation.RelationType, &relation.Evidence, &relation.Confidence, &relation.ApprovedBy,
		&relation.CreatedAt, &relation.UpdatedAt)
	return relation, err
}

func (r *Repository) UpsertRepositoryRelation(ctx context.Context, relation domain.RepositoryRelation) (domain.RepositoryRelation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.RepositoryRelation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO projects(id,display_name) VALUES($1,$1) ON CONFLICT (id) DO NOTHING`, relation.ProjectID); err != nil {
		return domain.RepositoryRelation{}, err
	}
	fromID, err := upsertSoftwareRepository(ctx, tx, relation.ProjectID, relation.From)
	if err != nil {
		return domain.RepositoryRelation{}, err
	}
	toID, err := upsertSoftwareRepository(ctx, tx, relation.ProjectID, relation.To)
	if err != nil {
		return domain.RepositoryRelation{}, err
	}
	if fromID == toID {
		return domain.RepositoryRelation{}, errors.New("a repository cannot relate to itself")
	}
	if relation.Confidence == 0 {
		relation.Confidence = 1
	}
	var relationID string
	err = tx.QueryRow(ctx, `INSERT INTO repository_relations(
        project_id,from_repository_id,to_repository_id,relation_type,evidence,confidence,approved_by
      ) VALUES($1,$2,$3,$4,$5,$6,$7)
      ON CONFLICT (project_id,from_repository_id,to_repository_id,relation_type)
      DO UPDATE SET evidence=EXCLUDED.evidence,confidence=EXCLUDED.confidence,
                    approved_by=EXCLUDED.approved_by,updated_at=now()
      RETURNING id::text`, relation.ProjectID, fromID, toID, relation.RelationType,
		relation.Evidence, relation.Confidence, relation.ApprovedBy).Scan(&relationID)
	if err != nil {
		return domain.RepositoryRelation{}, fmt.Errorf("upsert repository relation: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic) VALUES($1,'repository_relation.upsert')`, relationID); err != nil {
		return domain.RepositoryRelation{}, err
	}
	result, err := scanRelation(tx.QueryRow(ctx, `SELECT `+relationColumns+`
      FROM repository_relations r
      JOIN software_repositories f ON f.id=r.from_repository_id
      JOIN software_repositories t ON t.id=r.to_repository_id
      WHERE r.id::text=$1`, relationID))
	if err != nil {
		return domain.RepositoryRelation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.RepositoryRelation{}, err
	}
	return result, nil
}

func upsertSoftwareRepository(ctx context.Context, tx pgx.Tx, projectID string, repository domain.SoftwareRepository) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO software_repositories(
        project_id,name,canonical_url,default_branch,revision
      ) VALUES($1,$2,$3,$4,$5)
      ON CONFLICT (project_id,canonical_url)
      DO UPDATE SET name=EXCLUDED.name,
                    default_branch=CASE WHEN EXCLUDED.default_branch='' THEN software_repositories.default_branch ELSE EXCLUDED.default_branch END,
                    revision=CASE WHEN EXCLUDED.revision='' THEN software_repositories.revision ELSE EXCLUDED.revision END,
                    updated_at=now()
      RETURNING id::text`, projectID, repository.Name, repository.CanonicalURL,
		repository.DefaultBranch, repository.Revision).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("upsert software repository %q: %w", repository.CanonicalURL, err)
	}
	return id, nil
}

// UpsertSoftwareRepository registers repository identity independently from a
// relationship or code-analysis run. This keeps documentation-only and other
// unsupported-language repositories in the authoritative catalog even when
// they do not produce a code graph.
func (r *Repository) UpsertSoftwareRepository(ctx context.Context, projectID string, repository domain.SoftwareRepository) (domain.SoftwareRepository, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.SoftwareRepository{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO projects(id,display_name) VALUES($1,$1) ON CONFLICT (id) DO NOTHING`, projectID); err != nil {
		return domain.SoftwareRepository{}, err
	}
	id, err := upsertSoftwareRepository(ctx, tx, projectID, repository)
	if err != nil {
		return domain.SoftwareRepository{}, err
	}
	var result domain.SoftwareRepository
	if err := tx.QueryRow(ctx, `SELECT id::text,project_id,name,canonical_url,default_branch,revision,created_at,updated_at
      FROM software_repositories WHERE id::text=$1`, id).Scan(
		&result.ID, &result.ProjectID, &result.Name, &result.CanonicalURL,
		&result.DefaultBranch, &result.Revision, &result.CreatedAt, &result.UpdatedAt,
	); err != nil {
		return domain.SoftwareRepository{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SoftwareRepository{}, err
	}
	return result, nil
}

func (r *Repository) GetRepositoryRelation(ctx context.Context, id string) (domain.RepositoryRelation, error) {
	relation, err := scanRelation(r.pool.QueryRow(ctx, `SELECT `+relationColumns+`
      FROM repository_relations r
      JOIN software_repositories f ON f.id=r.from_repository_id
      JOIN software_repositories t ON t.id=r.to_repository_id
      WHERE r.id::text=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return relation, fmt.Errorf("repository relation %q not found", id)
	}
	return relation, err
}

func (r *Repository) GetRepositoryRelationsMany(ctx context.Context, ids []string) ([]domain.RepositoryRelation, error) {
	if len(ids) == 0 {
		return []domain.RepositoryRelation{}, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+relationColumns+`
      FROM repository_relations r
      JOIN software_repositories f ON f.id=r.from_repository_id
      JOIN software_repositories t ON t.id=r.to_repository_id
      WHERE r.id::text=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.RepositoryRelation, 0, len(ids))
	for rows.Next() {
		relation, err := scanRelation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, relation)
	}
	return result, rows.Err()
}

func (r *Repository) GetRepositoryGraph(ctx context.Context, projectID, root string, depth int) ([]domain.RepositoryRelation, error) {
	rows, err := r.pool.Query(ctx, `WITH RECURSIVE walk(id,path,depth) AS (
        SELECT id,ARRAY[id],0 FROM software_repositories
        WHERE project_id=$1 AND (id::text=$2 OR canonical_url=$2 OR name=$2)
        UNION ALL
        SELECT n.id,w.path || n.id,w.depth+1
        FROM walk w
        JOIN repository_relations edge
          ON edge.project_id=$1 AND (edge.from_repository_id=w.id OR edge.to_repository_id=w.id)
        JOIN software_repositories n
          ON n.id=CASE WHEN edge.from_repository_id=w.id THEN edge.to_repository_id ELSE edge.from_repository_id END
        WHERE w.depth < $3 AND NOT n.id=ANY(w.path)
      ), selected AS (SELECT DISTINCT id FROM walk)
      SELECT `+relationColumns+`
      FROM repository_relations r
      JOIN software_repositories f ON f.id=r.from_repository_id
      JOIN software_repositories t ON t.id=r.to_repository_id
      WHERE r.project_id=$1 AND r.from_repository_id IN (SELECT id FROM selected)
        AND r.to_repository_id IN (SELECT id FROM selected)
      ORDER BY f.name,r.relation_type,t.name`, projectID, root, depth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.RepositoryRelation, 0)
	for rows.Next() {
		relation, err := scanRelation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, relation)
	}
	return result, rows.Err()
}

func (r *Repository) RequeueRepositoryRelations(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic)
        SELECT id,'repository_relation.upsert' FROM repository_relations`)
	return result.RowsAffected(), err
}

func (r *Repository) ResolveSoftwareRepository(ctx context.Context, projectID, root string) (domain.SoftwareRepository, error) {
	var repository domain.SoftwareRepository
	err := r.pool.QueryRow(ctx, `SELECT id::text,project_id,name,canonical_url,default_branch,revision,created_at,updated_at
      FROM software_repositories
      WHERE project_id=$1 AND (id::text=$2 OR canonical_url=$2 OR name=$2)`, projectID, root).Scan(
		&repository.ID, &repository.ProjectID, &repository.Name, &repository.CanonicalURL,
		&repository.DefaultBranch, &repository.Revision, &repository.CreatedAt, &repository.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository, fmt.Errorf("repository %q not found in project %q", root, projectID)
	}
	return repository, err
}

func (r *Repository) GetSoftwareRepositoriesMany(ctx context.Context, projectID string, ids []string) ([]domain.SoftwareRepository, error) {
	if len(ids) == 0 {
		return []domain.SoftwareRepository{}, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text,project_id,name,canonical_url,default_branch,revision,created_at,updated_at
      FROM software_repositories WHERE project_id=$1 AND id::text=ANY($2) ORDER BY name,id`, projectID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.SoftwareRepository, 0, len(ids))
	for rows.Next() {
		var repository domain.SoftwareRepository
		if err := rows.Scan(&repository.ID, &repository.ProjectID, &repository.Name, &repository.CanonicalURL,
			&repository.DefaultBranch, &repository.Revision, &repository.CreatedAt, &repository.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, repository)
	}
	return result, rows.Err()
}

func (r *Repository) GetRepositoryRelationsForNodes(ctx context.Context, projectID string, ids []string) ([]domain.RepositoryRelation, error) {
	if len(ids) == 0 {
		return []domain.RepositoryRelation{}, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+relationColumns+`
      FROM repository_relations r
      JOIN software_repositories f ON f.id=r.from_repository_id
      JOIN software_repositories t ON t.id=r.to_repository_id
      WHERE r.project_id=$1 AND r.from_repository_id::text=ANY($2) AND r.to_repository_id::text=ANY($2)
      ORDER BY f.name,r.relation_type,t.name`, projectID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.RepositoryRelation, 0)
	for rows.Next() {
		relation, err := scanRelation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, relation)
	}
	return result, rows.Err()
}

func (r *Repository) RepositoryProjectionCurrent(ctx context.Context, projectID string, ids []string) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	var current bool
	err := r.pool.QueryRow(ctx, `SELECT
        count(*) = cardinality($2::text[])
        AND COALESCE(bool_and(head.status='ready' AND head.backend='apache-age'
                     AND head.revision=repository.revision
                     AND head.source_updated_at >= repository.updated_at),false)
        AND NOT EXISTS (
          SELECT 1 FROM repository_relations relation
          LEFT JOIN graph_projection_relations projected ON projected.relation_id=relation.id
          WHERE relation.project_id=$1
            AND (relation.from_repository_id::text=ANY($2) OR relation.to_repository_id::text=ANY($2))
            AND (projected.relation_id IS NULL OR projected.backend <> 'apache-age'
                 OR projected.status <> 'ready' OR projected.source_updated_at < relation.updated_at)
        )
      FROM software_repositories repository
      LEFT JOIN graph_repository_projection_heads head ON head.repository_id=repository.id
      WHERE repository.project_id=$1 AND repository.id::text=ANY($2)`, projectID, ids).Scan(&current)
	return current, err
}
