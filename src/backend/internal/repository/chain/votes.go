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

const validateVoteSourceQuery = `
	SELECT cp.position
	FROM exchange_offers AS source
	JOIN cluster_members AS member ON member.request_id = source.id
	JOIN chain_participants AS cp ON cp.cluster_id = member.cluster_id
	WHERE cp.chain_id = $1
	  AND source.id = $2
	  AND source.user_id = $3
	  AND source.status IN ('ACTIVE', 'IN_PROPOSAL')
`

const validateVoteTargetQuery = `
	SELECT cp.position
	FROM exchange_offers AS target
	JOIN cluster_members AS member ON member.request_id = target.id
	JOIN chain_participants AS cp ON cp.cluster_id = member.cluster_id
	WHERE cp.chain_id = $1
	  AND target.id = $2
	  AND target.status IN ('ACTIVE', 'IN_PROPOSAL')
`

const listPendingVoteEdgesQuery = `
	SELECT vote.request_id, vote.target_request_id, source_participant.position
	FROM votes AS vote
	JOIN cluster_members AS source_member ON source_member.request_id = vote.request_id
	JOIN chain_participants AS source_participant
	  ON source_participant.chain_id = vote.chain_id
	 AND source_participant.cluster_id = source_member.cluster_id
	WHERE vote.chain_id = $1
	  AND vote.vote = 'pending'
	ORDER BY source_participant.position, vote.request_id, vote.target_request_id
`

func (r *Postgres) LockForVote(
	ctx context.Context,
	tx database.Tx,
	chainID int64,
) (entity.ChainStatus, int, error) {
	var status entity.ChainStatus
	var length int
	err := tx.QueryRow(ctx, `
		SELECT status, length
		FROM chains
		WHERE id = $1
		FOR UPDATE
	`, chainID).Scan(&status, &length)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", 0, repository.MapDBErr(err)
		}
		return "", 0, entity.ErrChainNotFound
	}
	return status, length, nil
}

func (r *Postgres) ValidateVoteParticipants(
	ctx context.Context,
	tx database.Tx,
	userID string,
	chainID, requestID, targetRequestID int64,
	chainLength int,
) error {
	var sourcePosition int
	err := tx.QueryRow(ctx, validateVoteSourceQuery, chainID, requestID, userID).Scan(&sourcePosition)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return repository.MapDBErr(err)
		}
		return entity.ErrChainVoteForbidden
	}

	var targetPosition int
	err = tx.QueryRow(ctx, validateVoteTargetQuery, chainID, targetRequestID).Scan(&targetPosition)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return repository.MapDBErr(err)
		}
		return entity.ErrInvalidVoteTarget
	}
	if chainLength <= 0 || targetPosition != (sourcePosition+1)%chainLength {
		return entity.ErrInvalidVoteTarget
	}
	return nil
}

func (r *Postgres) GetVote(
	ctx context.Context,
	tx database.Tx,
	userID string,
	chainID, requestID, targetRequestID int64,
) (entity.ChainVote, error) {
	vote := entity.ChainVote{
		ChainID:         chainID,
		RequestID:       requestID,
		TargetRequestID: targetRequestID,
	}
	err := tx.QueryRow(ctx, `
		SELECT vote.vote, vote.voted_at
		FROM votes AS vote
		JOIN exchange_offers AS source ON source.id = vote.request_id
		WHERE vote.chain_id = $1
		  AND vote.request_id = $2
		  AND vote.target_request_id = $3
		  AND source.user_id = $4
	`, chainID, requestID, targetRequestID, userID).Scan(&vote.Vote, &vote.VotedAt)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return entity.ChainVote{}, repository.MapDBErr(err)
		}
		return entity.ChainVote{}, entity.ErrChainVoteForbidden
	}
	return vote, nil
}

func (r *Postgres) UpsertPendingVote(
	ctx context.Context,
	tx database.Tx,
	chainID, requestID, targetRequestID int64,
) (time.Time, error) {
	var votedAt time.Time
	err := tx.QueryRow(ctx, `
		INSERT INTO votes (chain_id, request_id, target_request_id, vote, voted_at)
		VALUES ($1, $2, $3, 'pending', NOW())
		ON CONFLICT ON CONSTRAINT votes_chain_request_target_key
		DO UPDATE SET
			vote = 'pending',
			voted_at = votes.voted_at
		RETURNING voted_at
	`, chainID, requestID, targetRequestID).Scan(&votedAt)
	if err != nil {
		return time.Time{}, repository.MapDBErr(err)
	}
	return votedAt, nil
}

// DeletePendingVote withdraws only a candidate-stage response. A missing row

func (r *Postgres) DeletePendingVote(
	ctx context.Context,
	tx database.Tx,
	chainID, requestID, targetRequestID int64,
) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM votes
		WHERE chain_id = $1
		  AND request_id = $2
		  AND target_request_id = $3
		  AND vote = 'pending'
	`, chainID, requestID, targetRequestID)
	if err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}

func (r *Postgres) ListPendingVoteEdges(
	ctx context.Context,
	tx database.Tx,
	chainID int64,
) ([]entity.VoteEdge, error) {
	rows, err := tx.Query(ctx, listPendingVoteEdgesQuery, chainID)
	if err != nil {
		return nil, repository.MapDBErr(err)
	}
	defer rows.Close()

	edges := make([]entity.VoteEdge, 0)
	for rows.Next() {
		var edge entity.VoteEdge
		if err := rows.Scan(&edge.RequestID, &edge.TargetRequestID, &edge.Position); err != nil {
			return nil, repository.MapDBErr(err)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, repository.MapDBErr(err)
	}
	return edges, nil
}

func (r *Postgres) MarkRequestInProposal(ctx context.Context, tx database.Tx, requestID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers
		SET status = 'IN_PROPOSAL', updated_at = NOW()
		WHERE id = $1 AND status IN ('ACTIVE', 'IN_PROPOSAL')
	`, requestID); err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}

func (r *Postgres) RestoreActiveIfNoPendingVotes(ctx context.Context, tx database.Tx, requestID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers AS eo
		SET status = 'ACTIVE', updated_at = NOW()
		WHERE eo.id = $1
		  AND eo.status = 'IN_PROPOSAL'
		  AND NOT EXISTS (
			SELECT 1 FROM votes AS v
			WHERE v.request_id = eo.id AND v.vote = 'pending'
		  )
	`, requestID); err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}
