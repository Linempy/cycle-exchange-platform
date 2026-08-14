package chain

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
)

func (r *Postgres) ListReplacementOptions(ctx context.Context, userID string, chainID int64) ([]entity.ReplacementOption, error) {
	eligibility := ReplacementEligibilitySQL("$1", "v.position", "item", "previous_offer", "next_item", "$3", true)

	rows, err := r.pool.Query(ctx, `
		WITH vacancy AS (
			SELECT cp.position, cp.cluster_id, cp.request_id, c.length
			FROM chain_participants AS cp
			JOIN chains AS c ON c.id = cp.chain_id
			JOIN exchange_offers AS selected ON selected.id = cp.request_id
			WHERE cp.chain_id = $1 AND c.status = 'PROPOSED'
			  AND selected.status = 'ACTIVE'
			  AND NOT EXISTS (SELECT 1 FROM votes v WHERE v.chain_id = cp.chain_id AND v.request_id = cp.request_id)
			LIMIT 1
		), actor AS (
			SELECT cp.request_id
			FROM chain_participants cp
			JOIN exchange_offers eo ON eo.id = cp.request_id
			CROSS JOIN vacancy v
			WHERE cp.chain_id = $1 AND cp.position = (v.position - 1 + v.length) % v.length
			  AND eo.user_id = $2
		)
		SELECT candidate.id, candidate.offered_item_id, item.title, COALESCE(item.description, ''),
		       COALESCE(candidate.wanted_description, ''), item.image_url,
		       COALESCE(position.reliability, 0.75), candidate.updated_at
		FROM vacancy v
		JOIN actor ON true
		JOIN cluster_members member ON member.cluster_id = v.cluster_id AND member.request_id <> v.request_id
		JOIN exchange_offers candidate ON candidate.id = member.request_id
		JOIN items item ON item.id = candidate.offered_item_id
		JOIN chain_participants position ON position.chain_id = $1 AND position.position = v.position
		JOIN chain_participants previous_cp ON previous_cp.chain_id = $1
		 AND previous_cp.position = (v.position - 1 + v.length) % v.length
		JOIN exchange_offers previous_offer ON previous_offer.id = previous_cp.request_id
		JOIN chain_participants next_cp ON next_cp.chain_id = $1
		 AND next_cp.position = (v.position + 1) % v.length
		JOIN exchange_offers next_offer ON next_offer.id = next_cp.request_id
		JOIN items next_item ON next_item.id = next_offer.offered_item_id
		WHERE `+eligibility+`
		ORDER BY COALESCE(position.reliability, 0.75) DESC, candidate.updated_at, candidate.id
	`, chainID, userID, r.matchingThreshold)
	if err != nil {
		return nil, repository.MapDBErr(err)
	}
	defer rows.Close()
	options := make([]entity.ReplacementOption, 0)
	for rows.Next() {
		var option entity.ReplacementOption
		if err := rows.Scan(&option.RequestID, &option.OfferedItemID, &option.Title, &option.Description,
			&option.WantedDescription, &option.ImageURL, &option.Reliability, &option.RespondedAt); err != nil {
			return nil, repository.MapDBErr(err)
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, repository.MapDBErr(err)
	}
	return options, nil
}

func (r *Postgres) SelectReplacement(ctx context.Context, tx database.Tx, userID string, chainID, replacementRequestID int64) error {
	var position, length int
	var oldRequestID, actorRequestID, nextRequestID int64
	err := tx.QueryRow(ctx, `
		SELECT vacancy.position, c.length, vacancy.request_id, actor.request_id, next_cp.request_id
		FROM chains c
		JOIN chain_participants vacancy ON vacancy.chain_id = c.id
		JOIN exchange_offers old_request ON old_request.id = vacancy.request_id
		JOIN chain_participants actor ON actor.chain_id = c.id
		JOIN exchange_offers actor_offer ON actor_offer.id = actor.request_id AND actor_offer.user_id = $2
		JOIN chain_participants next_cp ON next_cp.chain_id = c.id
		WHERE c.id = $1 AND c.status = 'PROPOSED'
		  AND old_request.status = 'ACTIVE'
		  AND actor.position = (vacancy.position - 1 + c.length) % c.length
		  AND next_cp.position = (vacancy.position + 1) % c.length
		LIMIT 1
	`, chainID, userID).Scan(&position, &length, &oldRequestID, &actorRequestID, &nextRequestID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return repository.MapDBErr(err)
		}
		return entity.ErrChainVoteForbidden
	}
	_ = length

	eligibility := ReplacementEligibilitySQL("$1", "$6", "candidate_item", "actor_offer", "next_item", "$7", true)

	var valid bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM cluster_members candidate_member
			JOIN cluster_members old_member ON old_member.cluster_id = candidate_member.cluster_id
			JOIN exchange_offers candidate ON candidate.id = candidate_member.request_id
			JOIN items candidate_item ON candidate_item.id = candidate.offered_item_id
			JOIN exchange_offers actor_offer ON actor_offer.id = $4
			JOIN exchange_offers next_offer ON next_offer.id = $5
			JOIN items next_item ON next_item.id = next_offer.offered_item_id
			WHERE old_member.request_id = $2 AND candidate.id = $3
			  AND `+eligibility+`
		)
	`,
		chainID,
		oldRequestID,
		replacementRequestID,
		actorRequestID,
		nextRequestID,
		position,
		r.matchingThreshold,
	).Scan(&valid); err != nil {
		return repository.MapDBErr(err)
	}
	if !valid {
		return entity.ErrInvalidVoteTarget
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO chain_replacement_attempts (chain_id, request_id, status)
		VALUES ($1, $2, 'INVITED')
	`, chainID, replacementRequestID); err != nil {
		return fmt.Errorf("record replacement invitation: %w", repository.MapDBErr(err))
	}

	if _, err := tx.Exec(ctx, `DELETE FROM votes WHERE chain_id = $1 AND request_id = $2`, chainID, actorRequestID); err != nil {
		return repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO votes (chain_id, request_id, target_request_id, vote, voted_at)
		VALUES ($1, $2, $3, 'pending', NOW())
	`, chainID, actorRequestID, replacementRequestID); err != nil {
		return repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers
		SET status = 'IN_PROPOSAL', updated_at = NOW()
		WHERE id = $1 AND status = 'LOCKED'
	`, actorRequestID); err != nil {
		return repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE chain_participants SET request_id = $3 WHERE chain_id = $1 AND position = $2`, chainID, position, replacementRequestID); err != nil {
		return repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE exchange_offers SET status = 'IN_PROPOSAL', updated_at = NOW() WHERE id = $1`, replacementRequestID); err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}
