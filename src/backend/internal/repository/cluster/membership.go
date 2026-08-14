package cluster

import (
	"context"
	"errors"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
	"github.com/jackc/pgx/v5"
)

func (r *Postgres) DeleteMembership(ctx context.Context, tx database.Tx, offerID int64) (*int64, error) {
	var clusterID int64
	err := tx.QueryRow(ctx, `
		DELETE FROM cluster_members
		WHERE request_id = $1
		RETURNING cluster_id
	`, offerID).Scan(&clusterID)
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

// AddMember добавляет предложение в кластер.
func (r *Postgres) AddMember(ctx context.Context, tx database.Tx, clusterID, offerID int64) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO cluster_members (cluster_id, request_id)
		VALUES ($1, $2)
	`, clusterID, offerID); err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return mappedErr
		}
		return err
	}
	return nil
}
