package chain

import (
	"context"
	"errors"
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
	"github.com/jackc/pgx/v5"
)

// MarkRequestLocked переводит заявку в жёсткую блокировку.
func (r *Postgres) MarkRequestLocked(ctx context.Context, tx database.Tx, requestID int64) error {
	result, err := tx.Exec(ctx, `
		UPDATE exchange_offers
		SET status = 'LOCKED', updated_at = NOW()
		WHERE id = $1 AND status IN ('IN_PROPOSAL', 'ACTIVE', 'LOCKED')
	`, requestID)
	if err != nil {
		return repository.MapDBErr(err)
	}
	if result.RowsAffected() != 1 {
		return entity.ErrChainNotProposed
	}
	return nil
}

// FreezeChain переводит цепочку в FROZEN с дедлайном и оптимистичной версией.
func (r *Postgres) FreezeChain(ctx context.Context, tx database.Tx, chainID int64, deadline time.Time) error {
	result, err := tx.Exec(ctx, `
		UPDATE chains
		SET status = 'FROZEN',
		    freeze_deadline_at = $2,
		    invalid_reason = NULL,
		    version = version + 1,
		    updated_at = NOW()
		WHERE id = $1 AND status = 'PROPOSED'
	`, chainID, deadline)
	if err != nil {
		return repository.MapDBErr(err)
	}
	if result.RowsAffected() != 1 {
		return entity.ErrChainNotProposed
	}
	return nil
}

// LockRequestsInChain переводит все заявки цепочки в LOCKED.
func (r *Postgres) LockRequestsInChain(ctx context.Context, tx database.Tx, chainID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers AS eo
		SET status = 'LOCKED', updated_at = NOW()
		WHERE eo.id IN (
			SELECT cp.request_id
			FROM chain_participants AS cp
			WHERE cp.chain_id = $1
		)
		  AND eo.status IN ('IN_PROPOSAL', 'ACTIVE')
	`, chainID); err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}

// MarkItemsUnavailable переводит предлагаемые товары участников в UNAVAILABLE.
func (r *Postgres) MarkItemsUnavailable(ctx context.Context, tx database.Tx, chainID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE items AS i
		SET status = 'UNAVAILABLE', updated_at = NOW()
		WHERE i.id IN (
			SELECT eo.offered_item_id
			FROM chain_participants AS cp
			JOIN exchange_offers AS eo ON eo.id = cp.request_id
			WHERE cp.chain_id = $1
		)
	`, chainID); err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}

// LockRequestsForFreeze сериализует подтверждения пересекающихся цепочек.
// Сортировка исключает взаимную блокировку при разном порядке участников.
func (r *Postgres) LockRequestsForFreeze(ctx context.Context, tx database.Tx, requestIDs []int64) error {
	rows, err := tx.Query(ctx, `
		SELECT id
		FROM exchange_offers
		WHERE id = ANY($1::bigint[])
		ORDER BY id
		FOR UPDATE
	`, requestIDs)
	if err != nil {
		return repository.MapDBErr(err)
	}
	defer rows.Close()

	for rows.Next() {
		var requestID int64
		if err := rows.Scan(&requestID); err != nil {
			return repository.MapDBErr(err)
		}
	}
	if err := rows.Err(); err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}

// LoadRequestLiveChainStatus вернёт FROZEN, если заявка уже сидит в замороженной цепочке.
func (r *Postgres) LoadRequestLiveChainStatus(ctx context.Context, tx database.Tx, requestID int64) (entity.ChainStatus, error) {
	var status entity.ChainStatus
	err := tx.QueryRow(ctx, `
		SELECT c.status
		FROM chain_participants AS cp
		JOIN chains AS c ON c.id = cp.chain_id
		WHERE cp.request_id = $1
		ORDER BY CASE WHEN c.status = 'FROZEN' THEN 0 ELSE 1 END
		LIMIT 1
	`, requestID).Scan(&status)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return entity.ChainStatus(""), repository.MapDBErr(err)
		}
		return entity.ChainStatusCandidate, nil
	}
	return status, nil
}

// ReleaseCompetitorsFromOtherChains вычёркивает участников замороженной цепочки
// из конкурирующих цепочек (удаляет их голоса и chain_participants) и возвращает
// chainID конкурирующих цепочек, где остались участники (нужно пересобрать).
// Сама замороженная цепочка chainID НЕ трогается.
// ReleaseUnselectedFromChain оставляет в замороженной цепочке только выбранный
// цикл из chain_participants и возвращает альтернативные заявки в активный поиск.
func (r *Postgres) ReleaseUnselectedFromChain(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT v.request_id
		FROM votes AS v
		WHERE v.chain_id = $1
		  AND NOT EXISTS (
			SELECT 1
			FROM chain_participants AS selected
			WHERE selected.chain_id = v.chain_id
			  AND selected.request_id = v.request_id
		  )
		ORDER BY v.request_id
	`, chainID)
	if err != nil {
		return nil, repository.MapDBErr(err)
	}
	released := make([]int64, 0)
	for rows.Next() {
		var requestID int64
		if err := rows.Scan(&requestID); err != nil {
			rows.Close()
			return nil, repository.MapDBErr(err)
		}
		released = append(released, requestID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, repository.MapDBErr(err)
	}
	rows.Close()

	if _, err := tx.Exec(ctx, `
		DELETE FROM votes AS v
		WHERE v.chain_id = $1
		  AND (
			NOT EXISTS (
				SELECT 1 FROM chain_participants AS source
				WHERE source.chain_id = v.chain_id AND source.request_id = v.request_id
			)
			OR NOT EXISTS (
				SELECT 1 FROM chain_participants AS target
				WHERE target.chain_id = v.chain_id AND target.request_id = v.target_request_id
			)
		  )
	`, chainID); err != nil {
		return nil, repository.MapDBErr(err)
	}

	for _, requestID := range released {
		if err := r.RestoreActiveIfNoPendingVotes(ctx, tx, requestID); err != nil {
			return nil, err
		}
	}
	return released, nil
}

func (r *Postgres) ReleaseCompetitorsFromOtherChains(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error) {

	if _, err := tx.Exec(ctx, `
		DELETE FROM votes AS v
		WHERE v.chain_id <> $1
		  AND EXISTS (
			SELECT 1
			FROM chain_participants AS frozen
			WHERE frozen.chain_id = $1
			  AND (frozen.request_id = v.request_id OR frozen.request_id = v.target_request_id)
		  )
	`, chainID); err != nil {
		return nil, repository.MapDBErr(err)
	}

	affected, err := func() ([]int64, error) {
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT cp_outside.chain_id
			FROM chain_participants AS cp_outside
			JOIN chain_participants AS frozen
			  ON frozen.chain_id = $1
			 AND (
				 frozen.request_id = cp_outside.request_id
				 OR (
					 frozen.cluster_id IS NOT NULL
					 AND frozen.cluster_id = cp_outside.cluster_id
				 )
			 )
			WHERE cp_outside.chain_id <> $1
		`, chainID)
		if err != nil {
			return nil, repository.MapDBErr(err)
		}
		defer rows.Close()
		ids := make([]int64, 0)
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, repository.MapDBErr(err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, repository.MapDBErr(err)
		}
		return ids, nil
	}()
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM chain_participants AS cp
		WHERE cp.chain_id <> $1
		  AND EXISTS (
			SELECT 1
			FROM chain_participants AS frozen
			WHERE frozen.chain_id = $1
			  AND frozen.request_id = cp.request_id
		  )
	`, chainID); err != nil {
		return nil, repository.MapDBErr(err)
	}

	return affected, nil
}
