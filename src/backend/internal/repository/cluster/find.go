package cluster

import (
	"context"
	"errors"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
	clusterservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/cluster"
	"github.com/jackc/pgx/v5"
)

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
				OR 1 - (member_item.embedding <=> $2::vector) < $6::float8 - $7::float8
				OR 1 - (member_offer.want_embedding <=> $3::vector) < $6::float8 - $7::float8
				OR (
					(1 - (member_item.embedding <=> $2::vector)) +
					(1 - (member_offer.want_embedding <=> $3::vector))
				) / 2 < $6::float8
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
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			if mappedErr, ok := repository.DBErrToErr(err); ok {
				return nil, mappedErr
			}
			return nil, err
		}
		return nil, nil
	}
	return &clusterID, nil
}

func (r *Postgres) ConsolidateCandidateClusters(
	ctx context.Context,
	tx database.Tx,
	targetClusterID int64,
	offerIDs []int64,
	vectors clusterservice.OfferVectors,
	threshold float64,
	directionMargin float64,
) error {
	if len(offerIDs) == 0 {
		return nil
	}

	rows, err := tx.Query(ctx, `
		WITH candidates AS (
			SELECT DISTINCT cm.cluster_id
			FROM cluster_members AS cm
			WHERE cm.request_id = ANY($1::bigint[])
		)
		SELECT candidate.cluster_id
		FROM candidates AS candidate
		WHERE candidate.cluster_id <> $2
		  AND NOT EXISTS (
			SELECT 1
			FROM cluster_members AS member
			JOIN exchange_offers AS member_offer ON member_offer.id = member.request_id
			JOIN items AS member_item ON member_item.id = member_offer.offered_item_id
			WHERE member.cluster_id = candidate.cluster_id
			  AND member_offer.status = 'ACTIVE'
			  AND member_item.status = 'ACTIVE'
			  AND (
				COALESCE(member_item.category, '') IS DISTINCT FROM $5
				OR COALESCE(member_offer.wanted_category, '') IS DISTINCT FROM $6
				OR 1 - (member_item.embedding <=> $3::vector) < $7::float8 - $8::float8
				OR 1 - (member_offer.want_embedding <=> $4::vector) < $7::float8 - $8::float8
				OR ((1 - (member_item.embedding <=> $3::vector)) +
				    (1 - (member_offer.want_embedding <=> $4::vector))) / 2 < $7::float8
				OR ($5 = '' AND
				    ((1 - (member_item.embedding <=> $3::vector)) +
				     (1 - (member_offer.want_embedding <=> $4::vector))) / 2 <
				    ((1 - (member_item.embedding <=> $4::vector)) +
				     (1 - (member_offer.want_embedding <=> $3::vector))) / 2 + $8::float8)
			  )
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM chain_participants AS cp
			JOIN chains AS c ON c.id = cp.chain_id
			WHERE cp.cluster_id = candidate.cluster_id
			  AND c.status <> 'CANDIDATE'
		  )
		ORDER BY candidate.cluster_id
	`, offerIDs, targetClusterID, vectors.OfferEmbedding, vectors.WantEmbedding,
		vectors.Category, vectors.WantedCategory, threshold, directionMargin)
	if err != nil {
		return err
	}
	defer rows.Close()

	sourceIDs := make([]int64, 0)
	for rows.Next() {
		var sourceID int64
		if err := rows.Scan(&sourceID); err != nil {
			return err
		}
		sourceIDs = append(sourceIDs, sourceID)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, sourceID := range sourceIDs {
		if _, err := tx.Exec(ctx, `
			DELETE FROM chains AS c
			WHERE c.status = 'CANDIDATE'
			  AND EXISTS (
				SELECT 1 FROM chain_participants AS cp
				WHERE cp.chain_id = c.id AND cp.cluster_id = $1
			  )
		`, sourceID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cluster_members SET cluster_id = $1 WHERE cluster_id = $2
		`, targetClusterID, sourceID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM clusters WHERE id = $1`, sourceID); err != nil {
			return err
		}
	}
	return nil
}
