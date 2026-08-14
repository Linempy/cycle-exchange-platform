package exchange_offer

import (
	"context"
	"errors"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"

	"github.com/jackc/pgx/v5"
)

func (r *Postgres) Get(ctx context.Context, userID string, requestID int64) (entity.ExchangeOffer, error) {
	const query = `
		SELECT id, user_id, offered_item_id, wanted_description, COALESCE(wanted_category, ''),
		       status, version, created_at, updated_at
		FROM exchange_offers
		WHERE id = $1 AND user_id = $2 AND status <> 'REMOVED'
	`

	request, err := scanExchangeOffer(r.pool.QueryRow(ctx, query, requestID, userID))
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			if mappedErr, ok := repository.DBErrToErr(err); ok {
				return entity.ExchangeOffer{}, mappedErr
			}
			return entity.ExchangeOffer{}, err
		}
		return entity.ExchangeOffer{}, entity.ErrExchangeOfferNotFound
	}

	return request, nil
}

func (r *Postgres) List(ctx context.Context, userID string) ([]entity.ExchangeOfferListItem, error) {

	const query = `
		SELECT er.id, er.user_id, er.offered_item_id, er.wanted_description, COALESCE(er.wanted_category, ''),
		       er.status, er.version, er.created_at,
		       er.updated_at, i.title
		FROM exchange_offers AS er
		JOIN items AS i ON i.id = er.offered_item_id
		WHERE er.user_id = $1 AND er.status <> 'REMOVED'
		ORDER BY er.created_at DESC, er.id DESC
	`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	defer rows.Close()

	requests := make([]entity.ExchangeOfferListItem, 0)
	for rows.Next() {
		var item entity.ExchangeOfferListItem
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.OfferedItemID,
			&item.WantedDescription,
			&item.WantedCategory,
			&item.Status,
			&item.Version,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.OfferedItemTitle,
		); err != nil {
			if mappedErr, ok := repository.DBErrToErr(err); ok {
				return nil, mappedErr
			}
			return nil, err
		}
		requests = append(requests, item)
	}

	if err := rows.Err(); err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}

	return requests, nil
}
