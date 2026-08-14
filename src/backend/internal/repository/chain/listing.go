package chain

import (
	"context"
	"fmt"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
)

const loadVisibleChainsQuery = `
	SELECT c.id, c.status, COALESCE(c.score, 0), c.length,
	       c.freeze_deadline_at, c.invalid_reason, c.version,
	       c.created_at, c.updated_at,
	       viewer.request_id, viewer.position
	FROM chains AS c
	JOIN LATERAL (
		SELECT member.request_id, cp.position, COALESCE(cp.cluster_id, 0) AS cluster_id
		FROM chain_participants AS cp
		JOIN cluster_members AS member ON member.cluster_id = cp.cluster_id
		JOIN exchange_offers AS eo ON eo.id = member.request_id
		WHERE cp.chain_id = c.id
		  AND (c.status = 'CANDIDATE' OR member.request_id = cp.request_id)
		  AND eo.user_id = $1
		  AND (
			(c.status = 'CANDIDATE' AND eo.status IN ('ACTIVE', 'IN_PROPOSAL'))
			OR (c.status <> 'CANDIDATE' AND member.request_id = cp.request_id
				AND eo.status IN ('IN_PROPOSAL', 'LOCKED', 'IN_PROGRESS', 'DONE'))
		  )
		ORDER BY cp.position
		LIMIT 1
	) AS viewer ON true
	WHERE c.status IN ('CANDIDATE', 'PROPOSED', 'FROZEN', 'IN_PROGRESS', 'COMPLETED')
		AND ($2::bigint = 0 OR c.id = $2)
		AND ($3::bigint = 0 OR viewer.request_id = $3)
	ORDER BY c.created_at DESC, c.id DESC
`

func (r *Postgres) List(ctx context.Context, userID string) ([]entity.Chain, error) {
	chains, err := r.loadVisibleChains(ctx, userID, 0, 0)
	if err != nil || len(chains) == 0 {
		return chains, err
	}
	if err := r.loadParticipants(ctx, chains); err != nil {
		return nil, err
	}
	return chains, nil
}

func (r *Postgres) ListForOffer(ctx context.Context, userID string, offerID int64) ([]entity.Chain, error) {
	var owned bool
	if err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM exchange_offers
			WHERE id = $1
			  AND user_id = $2
			  AND status <> 'REMOVED'
		)
	`, offerID, userID).Scan(&owned); err != nil {
		return nil, repository.MapDBErr(err)
	}
	if !owned {
		return nil, entity.ErrExchangeOfferNotFound
	}

	chains, err := r.loadVisibleChains(ctx, userID, 0, offerID)
	if err != nil || len(chains) == 0 {
		return chains, err
	}
	if err := r.loadParticipants(ctx, chains); err != nil {
		return nil, err
	}
	return chains, nil
}

// Get возвращает актуальную цепочку только её участнику.
func (r *Postgres) Get(ctx context.Context, userID string, chainID int64) (entity.Chain, error) {
	chains, err := r.loadVisibleChains(ctx, userID, chainID, 0)
	if err != nil {
		return entity.Chain{}, err
	}
	if len(chains) == 0 {
		return entity.Chain{}, entity.ErrChainNotFound
	}
	if err := r.loadParticipants(ctx, chains); err != nil {
		return entity.Chain{}, err
	}
	return chains[0], nil
}

func (r *Postgres) loadVisibleChains(ctx context.Context, userID string, chainID, offerID int64) ([]entity.Chain, error) {
	rows, err := r.pool.Query(ctx, loadVisibleChainsQuery, userID, chainID, offerID)
	if err != nil {
		return nil, repository.MapDBErr(err)
	}
	defer rows.Close()

	chains := make([]entity.Chain, 0)
	for rows.Next() {
		var chain entity.Chain
		if err := rows.Scan(
			&chain.ID,
			&chain.Status,
			&chain.Score,
			&chain.Length,
			&chain.FreezeDeadlineAt,
			&chain.InvalidReason,
			&chain.Version,
			&chain.CreatedAt,
			&chain.UpdatedAt,
			&chain.CurrentRequestID,
			&chain.CurrentPosition,
		); err != nil {
			return nil, repository.MapDBErr(err)
		}
		chains = append(chains, chain)
	}
	if err := rows.Err(); err != nil {
		return nil, repository.MapDBErr(err)
	}
	return chains, nil
}

func (r *Postgres) loadParticipants(ctx context.Context, chains []entity.Chain) error {
	chainIDs := make([]int64, len(chains))
	byID := make(map[int64]*entity.Chain, len(chains))
	for i := range chains {
		chainIDs[i] = chains[i].ID
		byID[chains[i].ID] = &chains[i]
		chains[i].Participants = make([]entity.ChainParticipant, 0, chains[i].Length)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT cp.id, cp.chain_id, COALESCE(cp.cluster_id, 0), member.request_id, cp.position,
		       eo.user_id, eo.offered_item_id, i.title, COALESCE(i.description, ''),
		       COALESCE(eo.wanted_description, ''), eo.status, cp.created_at,
		       i.image_url
		FROM chain_participants AS cp
		JOIN chains AS c ON c.id = cp.chain_id
		JOIN cluster_members AS member ON member.cluster_id = cp.cluster_id
		JOIN exchange_offers AS eo ON eo.id = member.request_id
		JOIN items AS i ON i.id = eo.offered_item_id
		WHERE cp.chain_id = ANY($1::bigint[])
		  AND (c.status = 'CANDIDATE' OR member.request_id = cp.request_id)
		  AND (
			(c.status = 'CANDIDATE' AND eo.status IN ('ACTIVE', 'IN_PROPOSAL'))
			OR (c.status <> 'CANDIDATE' AND eo.status IN ('ACTIVE', 'IN_PROPOSAL', 'LOCKED', 'IN_PROGRESS', 'DONE'))
		  )
		ORDER BY cp.chain_id, cp.position, member.request_id
	`, chainIDs)
	if err != nil {
		return repository.MapDBErr(err)
	}
	defer rows.Close()

	for rows.Next() {
		var participant entity.ChainParticipant
		if err := rows.Scan(
			&participant.ID,
			&participant.ChainID,
			&participant.ClusterID,
			&participant.RequestID,
			&participant.Position,
			&participant.OwnerUserID,
			&participant.OfferedItemID,
			&participant.OfferedItemTitle,
			&participant.OfferedItemDescription,
			&participant.WantedDescription,
			&participant.RequestStatus,
			&participant.CreatedAt,
			&participant.ImageURL,
		); err != nil {
			return repository.MapDBErr(err)
		}
		if chain := byID[participant.ChainID]; chain != nil {
			chain.Participants = append(chain.Participants, participant)
		}
	}
	if err := rows.Err(); err != nil {
		return repository.MapDBErr(err)
	}
	rows.Close()
	return r.loadParticipantVotes(ctx, chains)
}

func (r *Postgres) loadParticipantVotes(ctx context.Context, chains []entity.Chain) error {
	chainIDs := make([]int64, len(chains))
	byID := make(map[int64]*entity.Chain, len(chains))
	for i := range chains {
		chainIDs[i] = chains[i].ID
		byID[chains[i].ID] = &chains[i]
	}

	rows, err := r.pool.Query(ctx, `
		SELECT chain_id, request_id, target_request_id, vote
		FROM votes
		WHERE chain_id = ANY($1::bigint[])
	`, chainIDs)
	if err != nil {
		return repository.MapDBErr(err)
	}
	defer rows.Close()

	for rows.Next() {
		var chainID, requestID, targetRequestID int64
		var vote entity.VoteValue
		if err := rows.Scan(&chainID, &requestID, &targetRequestID, &vote); err != nil {
			return repository.MapDBErr(err)
		}
		chain := byID[chainID]
		if chain == nil {
			continue
		}
		for i := range chain.Participants {
			if chain.Participants[i].RequestID == targetRequestID {
				chain.Participants[i].Vote = &vote
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}

func (r *Postgres) HasDeadlineEvent(ctx context.Context, userID string, chainID int64) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM chain_deadline_events
			WHERE chain_id = $1 AND user_id = $2 AND reason = 'deadline_expired'
		)
	`, chainID, userID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check chain deadline event: %w", err)
	}
	return exists, nil
}
