package exchange_offer

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
)

func (r *Postgres) Update(ctx context.Context, tx database.Tx, request entity.ExchangeOffer, expectedVersion int64) (entity.ExchangeOffer, error) {
	if err := ensureMutableRequest(ctx, tx, request.ID, request.UserID, expectedVersion); err != nil {
		return entity.ExchangeOffer{}, err
	}

	if err := ensureActiveOwnedItem(ctx, tx, request.UserID, request.OfferedItemID); err != nil {
		return entity.ExchangeOffer{}, err
	}

	const query = `
		UPDATE exchange_offers
		SET offered_item_id = $3,
		    wanted_description = $4,
		    wanted_category = $5,
		    want_embedding = $6::vector,
		    version = version + 1,
		    updated_at = now()
		WHERE id = $1
		  AND user_id = $2
		  AND version = $7
		  AND status NOT IN ('IN_PROPOSAL', 'LOCKED', 'REMOVED')
		RETURNING id, user_id, offered_item_id, wanted_description, COALESCE(wanted_category, ''),
		          status, version, created_at, updated_at
	`

	updated, err := scanExchangeOffer(tx.QueryRow(
		ctx,
		query,
		request.ID,
		request.UserID,
		request.OfferedItemID,
		request.WantedDescription,
		request.WantedCategory,
		vectorLiteral(request.WantEmbedding),
		expectedVersion,
	))
	if err != nil {
		if mapped := mutationError(ctx, tx, request.ID, request.UserID, expectedVersion, err); mapped != nil {
			return entity.ExchangeOffer{}, mapped
		}
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return entity.ExchangeOffer{}, mappedErr
		}
		return entity.ExchangeOffer{}, err
	}

	if err := invalidateCandidateChains(ctx, tx, updated.ID, "request_changed"); err != nil {
		return entity.ExchangeOffer{}, err
	}
	return updated, nil
}

func (r *Postgres) Archive(ctx context.Context, tx database.Tx, userID string, requestID, expectedVersion int64) (entity.ExchangeOffer, error) {
	const query = `
		UPDATE exchange_offers
		SET status = 'REMOVED',
		    version = version + 1,
		    updated_at = now()
		WHERE id = $1
		  AND user_id = $2
		  AND version = $3
		  AND status NOT IN ('IN_PROPOSAL', 'LOCKED', 'REMOVED')
		RETURNING id, user_id, offered_item_id, wanted_description, COALESCE(wanted_category, ''),
		          status, version, created_at, updated_at
	`

	archived, err := scanExchangeOffer(tx.QueryRow(ctx, query, requestID, userID, expectedVersion))
	if err != nil {
		if mapped := mutationError(ctx, tx, requestID, userID, expectedVersion, err); mapped != nil {
			return entity.ExchangeOffer{}, mapped
		}
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return entity.ExchangeOffer{}, mappedErr
		}
		return entity.ExchangeOffer{}, err
	}

	if err := invalidateCandidateChains(ctx, tx, archived.ID, "request_archived"); err != nil {
		return entity.ExchangeOffer{}, err
	}
	return archived, nil
}
