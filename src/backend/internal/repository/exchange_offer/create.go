package exchange_offer

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
)

func (r *Postgres) Create(ctx context.Context, tx database.Tx, request entity.ExchangeOffer) (entity.ExchangeOffer, error) {
	if err := ensureActiveOwnedItem(ctx, tx, request.UserID, request.OfferedItemID); err != nil {
		return entity.ExchangeOffer{}, err
	}

	const query = `
		INSERT INTO exchange_offers (
			user_id, offered_item_id, wanted_description, wanted_category, want_embedding,
			status, version
		)
		VALUES ($1, $2, $3, $4, $5::vector, $6, 1)
		RETURNING id, status, version, created_at, updated_at
	`

	created := request
	err := tx.QueryRow(
		ctx,
		query,
		request.UserID,
		request.OfferedItemID,
		request.WantedDescription,
		request.WantedCategory,
		vectorLiteral(request.WantEmbedding),
		entity.RequestStatusActive,
	).Scan(
		&created.ID,
		&created.Status,
		&created.Version,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return entity.ExchangeOffer{}, mappedErr
		}
		return entity.ExchangeOffer{}, err
	}

	return created, nil
}
