package chain

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
)

// LoadChainRequestIDs возвращает заявки участников цепочки.
func (r *Postgres) LoadChainRequestIDs(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT cp.request_id
		FROM chain_participants AS cp
		WHERE cp.chain_id = $1
		ORDER BY cp.position
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
}

// LoadActiveChainRequestIDs returns only requests that can seed candidate
// rebuilding. A request locked by the chain being frozen must not be sent back
// through cluster synchronization.
func (r *Postgres) LoadActiveChainRequestIDs(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT cp.request_id
		FROM chain_participants AS cp
		JOIN exchange_offers AS eo ON eo.id = cp.request_id
		WHERE cp.chain_id = $1 AND eo.status = 'ACTIVE'
		ORDER BY cp.position
	`, chainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListChainsContainingRequest returns candidate chains whose position contains
// the request, including the request as an alternative member of that
// position's cluster. Live proposals and frozen deals must never be removed by
// candidate rebuilding.
func (r *Postgres) ListChainsContainingRequest(ctx context.Context, tx database.Tx, requestID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT cp.chain_id
		FROM chain_participants AS cp
		JOIN cluster_members AS member ON member.cluster_id = cp.cluster_id
		JOIN chains AS chain ON chain.id = cp.chain_id
		WHERE member.request_id = $1 AND chain.status = 'CANDIDATE'
	`, requestID)
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
}

// DeleteChain удаляет цепочку целиком каскадом: голоса, участники, саму цепочку.
func (r *Postgres) DeleteChain(ctx context.Context, tx database.Tx, chainID int64) error {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT request_id
		FROM (
			SELECT request_id
			FROM chain_participants
			WHERE chain_id = $1
			UNION
			SELECT request_id
			FROM votes
			WHERE chain_id = $1
		) AS affected_requests
	`, chainID)
	if err != nil {
		return repository.MapDBErr(err)
	}
	affectedRequestIDs := make([]int64, 0)
	for rows.Next() {
		var requestID int64
		if err := rows.Scan(&requestID); err != nil {
			rows.Close()
			return repository.MapDBErr(err)
		}
		affectedRequestIDs = append(affectedRequestIDs, requestID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return repository.MapDBErr(err)
	}
	rows.Close()

	if _, err := tx.Exec(ctx, `DELETE FROM votes WHERE chain_id = $1`, chainID); err != nil {
		return repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chain_participants WHERE chain_id = $1`, chainID); err != nil {
		return repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chains WHERE id = $1`, chainID); err != nil {
		return repository.MapDBErr(err)
	}
	// A request may have responded to more than one candidate chain. Deleting
	// one candidate removes its votes, but must release the soft lock only when
	// no pending vote remains in any other chain. Otherwise the UI can show an
	// IN_PROPOSAL request for which no live exchange option exists.
	for _, requestID := range affectedRequestIDs {
		if err := r.RestoreActiveIfNoPendingVotes(ctx, tx, requestID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRequestParticipation удаляет только участие заявки в цепочках и её голоса,
// не трогая сами цепочки (их последующей пересборкой занимается matcher).
func (r *Postgres) DeleteRequestParticipation(ctx context.Context, tx database.Tx, requestID int64) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM votes WHERE request_id = $1 OR target_request_id = $1
	`, requestID); err != nil {
		return repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM chain_participants WHERE request_id = $1
	`, requestID); err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}
