package cluster

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
)

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

func (r *Postgres) Refresh(ctx context.Context, tx database.Tx, clusterID int64) error {
	const deleteQuery = `
		DELETE FROM clusters AS c
		WHERE c.id = $1
		  AND NOT EXISTS (
			SELECT 1 FROM cluster_members AS cm WHERE cm.cluster_id = c.id
		)
	`
	if _, err := tx.Exec(ctx, deleteQuery, clusterID); err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return mappedErr
		}
		return err
	}

	if _, err := tx.Exec(ctx, refreshClusterQuery, clusterID); err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return mappedErr
		}
		return err
	}
	return nil
}
