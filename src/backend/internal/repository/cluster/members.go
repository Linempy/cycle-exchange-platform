package cluster

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
)

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
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
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
			if mappedErr, ok := repository.DBErrToErr(err); ok {
				return nil, mappedErr
			}
			return nil, err
		}
		members = append(members, offer)
	}
	if err := rows.Err(); err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	return members, nil
}
