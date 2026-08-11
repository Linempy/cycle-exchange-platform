// Package cluster содержит PostgreSQL-репозиторий кластеров предложений.
package cluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	clusterservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/cluster"
)

// Postgres управляет составом кластеров в переданной транзакции.
type Postgres struct {
	pool *pgxpool.Pool
}

const refreshClusterQuery = `
	WITH centroid AS (
		SELECT avg(i.embedding) AS value
		FROM cluster_members AS cm
		JOIN exchange_offers AS eo ON eo.id = cm.request_id
		JOIN items AS i ON i.id = eo.offered_item_id
		WHERE cm.cluster_id = $1
	), stats AS (
		SELECT COALESCE(max(i.embedding <=> centroid.value), 0) AS epsilon
		FROM cluster_members AS cm
		JOIN exchange_offers AS eo ON eo.id = cm.request_id
		JOIN items AS i ON i.id = eo.offered_item_id
		CROSS JOIN centroid
		WHERE cm.cluster_id = $1
	)
	UPDATE clusters AS c
	SET centroid_embedding = centroid.value,
	    epsilon = stats.epsilon,
	    updated_at = now()
	FROM centroid, stats
	WHERE c.id = $1
	  AND centroid.value IS NOT NULL
`

// NewRepository создаёт репозиторий кластеров с ограниченным Top-K поиском.
func NewRepository(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

// ListActiveMembers возвращает текущий состав активного кластера. Метод нужен
// сервисам поиска цепочек и быстрой замены, которые дополнительно проверяют
// точную совместимость конкретных предложений.
func (r *Postgres) ListActiveMembers(ctx context.Context, clusterID int64) ([]entity.ExchangeOffer, error) {
	const query = `
		SELECT eo.id, eo.user_id, eo.offered_item_id, eo.wanted_description,
		       eo.status, eo.version, eo.created_at, eo.updated_at
		FROM cluster_members AS cm
		JOIN exchange_offers AS eo ON eo.id = cm.request_id AND eo.status = 'ACTIVE'
		WHERE cm.cluster_id = $1
		ORDER BY eo.id
	`

	rows, err := r.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("list active cluster members: %w", err)
	}
	defer rows.Close()

	members := make([]entity.ExchangeOffer, 0)
	for rows.Next() {
		var offer entity.ExchangeOffer
		if err := rows.Scan(
			&offer.ID,
			&offer.UserID,
			&offer.OfferedItemID,
			&offer.WantedDescription,
			&offer.Status,
			&offer.Version,
			&offer.CreatedAt,
			&offer.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan active cluster member: %w", err)
		}
		members = append(members, offer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active cluster members: %w", err)
	}
	return members, nil
}

// LoadVectors загружает векторы направления обмена для предложения.
func (r *Postgres) LoadVectors(ctx context.Context, tx database.Tx, offerID int64) (clusterservice.OfferVectors, error) {
	var offerEmbedding *string
	var wantEmbedding *string
	var category string
	var wantedCategory string
	err := tx.QueryRow(ctx, `
		SELECT i.embedding::text,
		       eo.want_embedding::text,
		       COALESCE(i.category, ''),
		       COALESCE(eo.wanted_category, '')
		FROM exchange_offers AS eo
		JOIN items AS i ON i.id = eo.offered_item_id
		WHERE eo.id = $1 AND eo.status = 'ACTIVE'
	`, offerID).Scan(&offerEmbedding, &wantEmbedding, &category, &wantedCategory)
	if errors.Is(err, pgx.ErrNoRows) {
		return clusterservice.OfferVectors{}, entity.ErrExchangeOfferNotFound
	}
	if err != nil {
		return clusterservice.OfferVectors{}, fmt.Errorf("load offer vectors for clustering: %w", err)
	}
	if offerEmbedding == nil || wantEmbedding == nil {
		return clusterservice.OfferVectors{}, entity.ErrOfferEmbeddingMissing
	}
	return clusterservice.OfferVectors{
		OfferEmbedding: *offerEmbedding,
		WantEmbedding:  *wantEmbedding,
		Category:       category,
		WantedCategory: wantedCategory,
	}, nil
}

// DeleteMembership удаляет строку membership и возвращает прежний кластер.
func (r *Postgres) DeleteMembership(ctx context.Context, tx database.Tx, offerID int64) (*int64, error) {
	var clusterID int64
	err := tx.QueryRow(ctx, `
		DELETE FROM cluster_members
		WHERE request_id = $1
		RETURNING cluster_id
	`, offerID).Scan(&clusterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("remove offer from previous cluster: %w", err)
	}
	return &clusterID, nil
}

// FindClusterForCandidates одним запросом возвращает кластер первого кандидата
// в порядке релевантности, полученном от pgvector-поиска.
func (r *Postgres) FindClusterForCandidates(
	ctx context.Context,
	tx database.Tx,
	offerIDs []int64,
	vectors clusterservice.OfferVectors,
	threshold float64,
	directionMargin float64,
) (*int64, error) {
	if len(offerIDs) == 0 {
		return nil, nil
	}

	const query = `
		WITH candidates AS (
			SELECT request_id, position
			FROM unnest($1::bigint[]) WITH ORDINALITY AS candidate(request_id, position)
		), candidate_clusters AS (
			SELECT cm.cluster_id, min(candidate.position) AS position
			FROM candidates AS candidate
			JOIN cluster_members AS cm ON cm.request_id = candidate.request_id
			GROUP BY cm.cluster_id
		)
		SELECT candidate_cluster.cluster_id
		FROM candidate_clusters AS candidate_cluster
		WHERE NOT EXISTS (
			SELECT 1
			FROM cluster_members AS member
			JOIN exchange_offers AS member_offer ON member_offer.id = member.request_id
			JOIN items AS member_item ON member_item.id = member_offer.offered_item_id
			WHERE member.cluster_id = candidate_cluster.cluster_id
			  AND member_offer.status = 'ACTIVE'
			  AND member_item.status = 'ACTIVE'
			  AND (
				COALESCE(member_item.category, '') IS DISTINCT FROM $4
				OR COALESCE(member_offer.wanted_category, '') IS DISTINCT FROM $5
				OR 1 - (member_item.embedding <=> $2::vector) < $6
				OR 1 - (member_offer.want_embedding <=> $3::vector) < $6
				OR (
					$4 = ''
					AND (
						(1 - (member_item.embedding <=> $2::vector)) +
						(1 - (member_offer.want_embedding <=> $3::vector))
					) / 2 < (
						(1 - (member_item.embedding <=> $3::vector)) +
						(1 - (member_offer.want_embedding <=> $2::vector))
					) / 2 + $7
				)
			  )
		)
		ORDER BY candidate_cluster.position
		LIMIT 1
	`

	var clusterID int64
	err := tx.QueryRow(
		ctx,
		query,
		offerIDs,
		vectors.OfferEmbedding,
		vectors.WantEmbedding,
		vectors.Category,
		vectors.WantedCategory,
		threshold,
		directionMargin,
	).Scan(&clusterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find cluster for similar offers: %w", err)
	}
	return &clusterID, nil
}

// Create создаёт пустой кластер; участника добавляет вызывающий сервис.
func (r *Postgres) Create(ctx context.Context, tx database.Tx) (int64, error) {
	var clusterID int64
	err := tx.QueryRow(ctx, `
		INSERT INTO clusters (epsilon)
		VALUES (0)
		RETURNING id
	`).Scan(&clusterID)
	if err != nil {
		return 0, fmt.Errorf("create cluster: %w", err)
	}
	return clusterID, nil
}

// AddMember добавляет предложение в кластер.
func (r *Postgres) AddMember(ctx context.Context, tx database.Tx, clusterID, offerID int64) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO cluster_members (cluster_id, request_id)
		VALUES ($1, $2)
	`, clusterID, offerID); err != nil {
		return fmt.Errorf("add offer to cluster: %w", err)
	}
	return nil
}

// Refresh пересчитывает агрегаты кластера или удаляет его, если он пуст.
func (r *Postgres) Refresh(ctx context.Context, tx database.Tx, clusterID int64) error {
	const deleteQuery = `
		DELETE FROM clusters AS c
		WHERE c.id = $1
		  AND NOT EXISTS (
			SELECT 1 FROM cluster_members AS cm WHERE cm.cluster_id = c.id
		)
	`
	if _, err := tx.Exec(ctx, deleteQuery, clusterID); err != nil {
		return fmt.Errorf("delete empty cluster: %w", err)
	}

	if _, err := tx.Exec(ctx, refreshClusterQuery, clusterID); err != nil {
		return fmt.Errorf("refresh cluster centroid: %w", err)
	}
	return nil
}

var _ clusterservice.Repository = (*Postgres)(nil)
