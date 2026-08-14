package exchange_offer

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"

	"github.com/jackc/pgx/v5"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func scanExchangeOffer(row rowScanner) (entity.ExchangeOffer, error) {
	var request entity.ExchangeOffer
	err := row.Scan(
		&request.ID,
		&request.UserID,
		&request.OfferedItemID,
		&request.WantedDescription,
		&request.WantedCategory,
		&request.Status,
		&request.Version,
		&request.CreatedAt,
		&request.UpdatedAt,
	)
	return request, err
}

func ensureActiveOwnedItem(ctx context.Context, tx database.Tx, userID string, itemID int64) error {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM items
			WHERE id = $1
			  AND owner_user_id = $2
			  AND status = 'ACTIVE'
		)
	`

	var exists bool
	if err := tx.QueryRow(ctx, query, itemID, userID).Scan(&exists); err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return mappedErr
		}
		return err
	}
	if !exists {
		return entity.ErrOfferedItemUnavailable
	}
	return nil
}

func ensureMutableRequest(ctx context.Context, tx database.Tx, requestID int64, userID string, expectedVersion int64) error {
	var status entity.RequestStatus
	var currentVersion int64
	err := tx.QueryRow(ctx, `
		SELECT status, version
		FROM exchange_offers
		WHERE id = $1 AND user_id = $2
		FOR UPDATE
	`, requestID, userID).Scan(&status, &currentVersion)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			if mappedErr, ok := repository.DBErrToErr(err); ok {
				return mappedErr
			}
			return err
		}
		return entity.ErrExchangeOfferNotFound
	}
	if status == entity.RequestStatusInProposal || status == entity.RequestStatusLocked {
		return entity.ErrExchangeOfferLocked
	}
	if status == entity.RequestStatusRemoved {
		return entity.ErrExchangeOfferNotFound
	}
	if currentVersion != expectedVersion {
		return entity.ErrExchangeOfferVersionConflict
	}
	return nil
}

func invalidateCandidateChains(ctx context.Context, tx database.Tx, requestID int64, reason string) error {
	const query = `
		UPDATE chains AS c
		SET status = 'BROKEN',
		    invalid_reason = $2,
		    version = version + 1,
		    updated_at = now()
		WHERE c.status = 'CANDIDATE'
		  AND EXISTS (
			SELECT 1
			FROM chain_participants AS cp
			JOIN cluster_members AS member ON member.cluster_id = cp.cluster_id
			WHERE cp.chain_id = c.id
			  AND member.request_id = $1
		)
	`
	if _, err := tx.Exec(ctx, query, requestID, reason); err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return mappedErr
		}
		return err
	}
	return nil
}

func mutationError(ctx context.Context, tx database.Tx, requestID int64, userID string, expectedVersion int64, original error) error {
	if !errors.Is(original, pgx.ErrNoRows) {
		return nil
	}

	var status entity.RequestStatus
	var currentVersion int64
	err := tx.QueryRow(ctx, `
		SELECT status, version
		FROM exchange_offers
		WHERE id = $1 AND user_id = $2
	`, requestID, userID).Scan(&status, &currentVersion)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			if mappedErr, ok := repository.DBErrToErr(err); ok {
				return mappedErr
			}
			return err
		}
		return entity.ErrExchangeOfferNotFound
	}
	if status == entity.RequestStatusInProposal || status == entity.RequestStatusLocked {
		return entity.ErrExchangeOfferLocked
	}
	if currentVersion != expectedVersion {
		return entity.ErrExchangeOfferVersionConflict
	}
	return entity.ErrExchangeOfferNotFound
}

func vectorLiteral(vector []float32) string {
	parts := make([]string, len(vector))
	for i, value := range vector {
		parts[i] = strconv.FormatFloat(float64(value), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
