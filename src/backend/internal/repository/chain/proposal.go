package chain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/utils/ranker"
	"github.com/jackc/pgx/v5"
)

func (r *Postgres) Propose(
	ctx context.Context,
	tx database.Tx,
	chainID int64,
	requestIDsByPosition []int64,
	confirmationDeadline time.Time,
) error {
	for position, requestID := range requestIDsByPosition {
		result, err := tx.Exec(ctx, `
			UPDATE chain_participants
			SET request_id = $3
			WHERE chain_id = $1
			  AND position = $2
		`, chainID, position, requestID)
		if err != nil {
			return fmt.Errorf("pin chain participant at position %d: %w", position, repository.MapDBErr(err))
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("pin chain participant at position %d: %w", position, entity.ErrInvalidVoteTarget)
		}
	}

	result, err := tx.Exec(ctx, `
		UPDATE chains
		SET status = 'PROPOSED',
		    freeze_deadline_at = $2,
		    version = version + 1,
		    updated_at = NOW()
		WHERE id = $1
		  AND status = 'CANDIDATE'
	`, chainID, confirmationDeadline)
	if err != nil {
		return repository.MapDBErr(err)
	}
	if result.RowsAffected() != 1 {
		return entity.ErrChainNotCandidate
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chain_replacement_attempts WHERE chain_id = $1`, chainID); err != nil {
		return fmt.Errorf("clear replacement attempts before proposal: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers
		SET status = 'IN_PROPOSAL', updated_at = NOW()
		WHERE id = ANY($1::bigint[])
		  AND status IN ('ACTIVE', 'IN_PROPOSAL')
	`, requestIDsByPosition); err != nil {
		return repository.MapDBErr(err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE votes
		SET vote = 'pending', voted_at = NOW()
		WHERE chain_id = $1
		  AND vote <> 'pending'
	`, chainID); err != nil {
		return repository.MapDBErr(err)
	}

	return nil
}

// ExpireProposalIfDue лениво снимает просроченную мягкую блокировку.
// Условие на дедлайн не даёт нескольким одновременным запросам откатить цепочку повторно.
func (r *Postgres) ExpireProposalIfDue(ctx context.Context, tx database.Tx, chainID int64) (bool, error) {
	result, err := tx.Exec(ctx, `
		UPDATE chains
		SET status = 'CANDIDATE',
		    freeze_deadline_at = NULL,
		    invalid_reason = 'deadline_expired',
		    version = version + 1,
		    updated_at = NOW()
		WHERE id = $1
		  AND status = 'PROPOSED'
		  AND freeze_deadline_at <= NOW()
	`, chainID)
	if err != nil {
		return false, fmt.Errorf("expire proposed chain: %w", err)
	}
	if result.RowsAffected() == 0 {
		return false, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers AS eo
		SET status = 'ACTIVE', updated_at = NOW()
		WHERE eo.id IN (
			SELECT cp.request_id
			FROM chain_participants AS cp
			WHERE cp.chain_id = $1
		)
		  AND eo.status IN ('IN_PROPOSAL', 'LOCKED')
	`, chainID); err != nil {
		return false, fmt.Errorf("release expired proposal requests: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE items AS item
		SET status = 'ACTIVE', updated_at = NOW()
		WHERE item.id IN (
			SELECT offer.offered_item_id
			FROM chain_participants participant
			JOIN exchange_offers offer ON offer.id = participant.request_id
			WHERE participant.chain_id = $1
		) AND item.status = 'UNAVAILABLE'
	`, chainID); err != nil {
		return false, fmt.Errorf("release expired proposal items: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE votes
		SET vote = 'pending', voted_at = NOW()
		WHERE chain_id = $1
		  AND vote IN ('approved', 'thinking')
	`, chainID); err != nil {
		return false, fmt.Errorf("reset expired proposal confirmations: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chain_replacement_attempts WHERE chain_id = $1`, chainID); err != nil {
		return false, fmt.Errorf("clear expired replacement attempts: %w", err)
	}
	return true, nil
}

// ListExpiredChainIDs returns live chains whose current-stage deadline has
// elapsed. Callers still lock and re-check every chain while applying the
// transition, so concurrent API requests remain idempotent.
func (r *Postgres) ListExpiredChainIDs(ctx context.Context, tx database.Tx) ([]int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT id
		FROM chains
		WHERE status IN ('PROPOSED', 'FROZEN')
		  AND freeze_deadline_at IS NOT NULL
		  AND freeze_deadline_at <= NOW()
		ORDER BY id
		FOR UPDATE SKIP LOCKED
	`)
	if err != nil {
		return nil, fmt.Errorf("list expired chains: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan expired chain: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired chains: %w", err)
	}
	return ids, nil
}

// ExpireFrozenIfDue breaks an expired hard reservation, releases its requests
// and items, and returns requests that must be fed back into matching. Broken
// history keeps the aggregate row and invalid reason. Its technical participant
// rows are removed so clustering can safely rebuild or remove old clusters.
func (r *Postgres) ExpireFrozenIfDue(
	ctx context.Context,
	tx database.Tx,
	chainID int64,
) ([]int64, bool, error) {
	result, err := tx.Exec(ctx, `
		UPDATE chains
		SET status = 'BROKEN',
		    freeze_deadline_at = NULL,
		    invalid_reason = 'deadline_expired',
		    version = version + 1,
		    updated_at = NOW()
		WHERE id = $1
		  AND status = 'FROZEN'
		  AND freeze_deadline_at <= NOW()
	`, chainID)
	if err != nil {
		return nil, false, fmt.Errorf("expire frozen chain: %w", err)
	}
	if result.RowsAffected() == 0 {
		return nil, false, nil
	}

	requestIDs, err := r.LoadChainRequestIDs(ctx, tx, chainID)
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO chain_deadline_events (chain_id, user_id, reason)
		SELECT DISTINCT cp.chain_id, offer.user_id, 'deadline_expired'
		FROM chain_participants cp
		JOIN exchange_offers offer ON offer.id = cp.request_id
		WHERE cp.chain_id = $1
		ON CONFLICT DO NOTHING
	`, chainID); err != nil {
		return nil, false, fmt.Errorf("record expired chain notification: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers
		SET status = 'ACTIVE', updated_at = NOW()
		WHERE id = ANY($1::bigint[])
		  AND status = 'LOCKED'
	`, requestIDs); err != nil {
		return nil, false, fmt.Errorf("release expired frozen requests: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE items AS item
		SET status = 'ACTIVE', updated_at = NOW()
		WHERE item.status = 'UNAVAILABLE'
		  AND item.id IN (
			SELECT offer.offered_item_id
			FROM exchange_offers AS offer
			WHERE offer.id = ANY($1::bigint[])
		  )
	`, requestIDs); err != nil {
		return nil, false, fmt.Errorf("release expired frozen items: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM votes WHERE chain_id = $1`, chainID); err != nil {
		return nil, false, fmt.Errorf("delete expired frozen votes: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chain_participants WHERE chain_id = $1`, chainID); err != nil {
		return nil, false, fmt.Errorf("delete expired frozen participants: %w", err)
	}
	return requestIDs, true, nil
}

// LoadScoreFeatures возвращает сырые фичи score по позициям цепочки.
func (r *Postgres) LoadScoreFeatures(
	ctx context.Context, tx database.Tx, chainID int64,
) ([]float64, []float64, []int, error) {
	rows, err := tx.Query(ctx, `
		SELECT COALESCE(edge_cosine, 0), COALESCE(reliability, 0), COALESCE(cluster_size, 1)
		FROM chain_participants
		WHERE chain_id = $1
		ORDER BY position
	`, chainID)
	if err != nil {
		return nil, nil, nil, repository.MapDBErr(err)
	}
	defer rows.Close()

	var cosines []float64
	var reliability []float64
	var sizes []int
	for rows.Next() {
		var c, rel float64
		var size int
		if err := rows.Scan(&c, &rel, &size); err != nil {
			return nil, nil, nil, repository.MapDBErr(err)
		}
		cosines = append(cosines, c)
		reliability = append(reliability, rel)
		sizes = append(sizes, size)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, repository.MapDBErr(err)
	}
	return cosines, reliability, sizes, nil
}

func mapTxErr(err error) error {
	return repository.MapDBErr(err)
}

func (r *Postgres) loadCategoryCatalog(ctx context.Context, tx database.Tx) (map[string]int, int, error) {
	rows, err := tx.Query(ctx, `
		SELECT COALESCE(category, ''), COUNT(*)::int
		FROM items
		WHERE status = 'ACTIVE' AND COALESCE(category, '') <> ''
		GROUP BY 1
	`)
	if err != nil {
		return nil, 0, mapTxErr(err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	total := 0
	for rows.Next() {
		var cat string
		var n int
		if err := rows.Scan(&cat, &n); err != nil {
			return nil, 0, mapTxErr(err)
		}
		counts[cat] = n
		total += n
	}
	if err := rows.Err(); err != nil {
		return nil, 0, mapTxErr(err)
	}
	return counts, total, nil
}

// LoadRankerContext читает created_at, таймстампы голосов и категории участников цепочки.
func (r *Postgres) LoadRankerContext(
	ctx context.Context, tx database.Tx, chainID int64,
) (ranker.ContextSnapshot, error) {
	var snap ranker.ContextSnapshot
	if err := tx.QueryRow(ctx, `
		SELECT created_at FROM chains WHERE id = $1
	`, chainID).Scan(&snap.CreatedAt); err != nil {
		return ranker.ContextSnapshot{}, mapTxErr(err)
	}

	voteRows, err := tx.Query(ctx, `
		SELECT COALESCE(voted_at, created_at)
		FROM votes
		WHERE chain_id = $1
		ORDER BY COALESCE(voted_at, created_at)
	`, chainID)
	if err != nil {
		return ranker.ContextSnapshot{}, mapTxErr(err)
	}
	defer voteRows.Close()
	for voteRows.Next() {
		var ts time.Time
		if err := voteRows.Scan(&ts); err != nil {
			return ranker.ContextSnapshot{}, mapTxErr(err)
		}
		if !ts.IsZero() {
			snap.VoteTimes = append(snap.VoteTimes, ts)
		}
	}
	if err := voteRows.Err(); err != nil {
		return ranker.ContextSnapshot{}, mapTxErr(err)
	}
	voteRows.Close()

	catRows, err := tx.Query(ctx, `
		SELECT COALESCE(item.category, ''), COALESCE(offer.wanted_category, '')
		FROM chain_participants AS part
		JOIN exchange_offers AS offer ON offer.id = part.request_id
		JOIN items AS item ON item.id = offer.offered_item_id
		WHERE part.chain_id = $1
		ORDER BY part.position
	`, chainID)
	if err != nil {
		return ranker.ContextSnapshot{}, mapTxErr(err)
	}
	defer catRows.Close()
	for catRows.Next() {
		var offered, wanted string
		if err := catRows.Scan(&offered, &wanted); err != nil {
			return ranker.ContextSnapshot{}, mapTxErr(err)
		}
		snap.OfferedCategories = append(snap.OfferedCategories, offered)
		snap.WantedCategories = append(snap.WantedCategories, wanted)
	}
	if err := catRows.Err(); err != nil {
		return ranker.ContextSnapshot{}, mapTxErr(err)
	}
	catRows.Close()

	counts, total, err := r.loadCategoryCatalog(ctx, tx)
	if err != nil {
		return ranker.ContextSnapshot{}, err
	}
	snap.CategoryCounts = counts
	snap.CategoryTotal = total
	snap.StageEnteredAt = snap.CreatedAt
	if n := len(snap.VoteTimes); n > 0 {
		snap.StageEnteredAt = snap.VoteTimes[n-1]
	}
	return snap, nil
}

// LoadRankerContextForRequests — то же для ещё не сохранённого драфта (ADD).
func (r *Postgres) LoadRankerContextForRequests(
	ctx context.Context, tx database.Tx, requestIDs []int64,
) (ranker.ContextSnapshot, error) {
	var snap ranker.ContextSnapshot
	counts, total, err := r.loadCategoryCatalog(ctx, tx)
	if err != nil {
		return ranker.ContextSnapshot{}, err
	}
	snap.CategoryCounts = counts
	snap.CategoryTotal = total
	if len(requestIDs) == 0 {
		return snap, nil
	}

	rows, err := tx.Query(ctx, `
		SELECT offer.id, COALESCE(item.category, ''), COALESCE(offer.wanted_category, '')
		FROM exchange_offers AS offer
		JOIN items AS item ON item.id = offer.offered_item_id
		WHERE offer.id = ANY($1)
	`, requestIDs)
	if err != nil {
		return ranker.ContextSnapshot{}, mapTxErr(err)
	}
	defer rows.Close()

	byID := make(map[int64][2]string, len(requestIDs))
	for rows.Next() {
		var id int64
		var offered, wanted string
		if err := rows.Scan(&id, &offered, &wanted); err != nil {
			return ranker.ContextSnapshot{}, mapTxErr(err)
		}
		byID[id] = [2]string{offered, wanted}
	}
	if err := rows.Err(); err != nil {
		return ranker.ContextSnapshot{}, mapTxErr(err)
	}

	snap.OfferedCategories = make([]string, 0, len(requestIDs))
	snap.WantedCategories = make([]string, 0, len(requestIDs))
	for _, id := range requestIDs {
		pair := byID[id]
		snap.OfferedCategories = append(snap.OfferedCategories, pair[0])
		snap.WantedCategories = append(snap.WantedCategories, pair[1])
	}
	return snap, nil
}

// CountPendingVoters возвращает число откликнувшихся участников цепочки.
func (r *Postgres) CountPendingVoters(ctx context.Context, tx database.Tx, chainID int64) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(DISTINCT vote.request_id)
		FROM votes AS vote
		JOIN chain_participants AS source
		  ON source.chain_id = vote.chain_id AND source.request_id = vote.request_id
		JOIN chain_participants AS target
		  ON target.chain_id = vote.chain_id AND target.request_id = vote.target_request_id
		WHERE vote.chain_id = $1 AND vote.vote = 'pending'
	`, chainID).Scan(&count); err != nil {
		return 0, repository.MapDBErr(err)
	}
	return count, nil
}

// UpdateScore актуализирует score цепочки, сохраняя оптимистичную блокировку.
func (r *Postgres) UpdateScore(ctx context.Context, tx database.Tx, chainID int64, score float64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE chains
		SET score = $2, version = version + 1, updated_at = NOW()
		WHERE id = $1
	`, chainID, score); err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}

// ConfirmParticipant помечает голос участника как approved (идемпотентно).
func (r *Postgres) ConfirmParticipant(ctx context.Context, tx database.Tx, chainID, requestID, targetRequestID int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO votes (chain_id, request_id, target_request_id, vote, voted_at)
		VALUES ($1, $2, $3, 'approved', NOW())
		ON CONFLICT ON CONSTRAINT votes_chain_request_target_key
		DO UPDATE SET vote = 'approved', voted_at = NOW()
	`, chainID, requestID, targetRequestID)
	if err != nil {
		return repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE chain_replacement_attempts
		SET status = 'ACCEPTED', updated_at = NOW()
		WHERE chain_id = $1 AND request_id = $2 AND status = 'INVITED'
	`, chainID, requestID); err != nil {
		return fmt.Errorf("accept replacement invitation: %w", err)
	}
	return nil
}

// MarkParticipantThinking records an explicit decision to wait until the
// confirmation deadline. It is distinct from pending, which means no decision.
func (r *Postgres) MarkParticipantThinking(ctx context.Context, tx database.Tx, chainID, requestID, targetRequestID int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO votes (chain_id, request_id, target_request_id, vote, voted_at)
		VALUES ($1, $2, $3, 'thinking', NOW())
		ON CONFLICT ON CONSTRAINT votes_chain_request_target_key
		DO UPDATE SET vote = 'thinking', voted_at = NOW()
	`, chainID, requestID, targetRequestID)
	if err != nil {
		return repository.MapDBErr(err)
	}
	return nil
}

// UnconfirmParticipant returns an approved vote to pending without removing
// the participant from the proposal. Repeated calls are idempotent.
func (r *Postgres) UnconfirmParticipant(ctx context.Context, tx database.Tx, chainID, requestID, targetRequestID int64) error {
	result, err := tx.Exec(ctx, `
		UPDATE votes
		SET vote = 'pending', voted_at = NOW()
		WHERE chain_id = $1 AND request_id = $2 AND target_request_id = $3
		  AND vote IN ('approved', 'pending')
	`, chainID, requestID, targetRequestID)
	if err != nil {
		return fmt.Errorf("unconfirm participant: %w", err)
	}
	if result.RowsAffected() == 0 {
		return entity.ErrChainConfirmationNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers
		SET status = 'IN_PROPOSAL', updated_at = NOW()
		WHERE id = $1 AND status = 'LOCKED'
	`, requestID); err != nil {
		return fmt.Errorf("soften participant lock: %w", err)
	}
	return nil
}

// PrepareFrozenReplacement reopens a frozen chain for a short, restricted
// replacement round. invalid_reason distinguishes it from an ordinary proposal
// so a second withdrawal can atomically cancel the whole round.
func (r *Postgres) PrepareFrozenReplacement(ctx context.Context, tx database.Tx, chainID int64, deadline time.Time) error {
	result, err := tx.Exec(ctx, `
		UPDATE chains
		SET status = 'PROPOSED', freeze_deadline_at = $2,
		    invalid_reason = 'frozen_replacement', version = version + 1, updated_at = NOW()
		WHERE id = $1 AND status = 'FROZEN'
	`, chainID, deadline)
	if err != nil {
		return fmt.Errorf("prepare frozen replacement: %w", err)
	}
	if result.RowsAffected() != 1 {
		return entity.ErrChainNotFrozen
	}
	return nil
}

func (r *Postgres) IsFrozenReplacement(ctx context.Context, tx database.Tx, chainID int64) (bool, error) {
	var active bool
	if err := tx.QueryRow(ctx, `
		SELECT status = 'PROPOSED'
		   AND COALESCE(invalid_reason = 'frozen_replacement', FALSE)
		FROM chains WHERE id = $1
	`, chainID).Scan(&active); err != nil {
		return false, fmt.Errorf("check frozen replacement: %w", err)
	}
	return active, nil
}

// DeclineParticipant removes the participant's confirmation and releases its
// request. A replacement is allowed only when every other participant has
// confirmed and the declining participant has an own vote in the chain. An
// invited replacement has no own vote yet. Its refusal keeps the proposal open
// while an untried compatible request remains in the same cluster.
func (r *Postgres) DeclineParticipant(ctx context.Context, tx database.Tx, chainID, requestID int64, fastReplacementEligible bool) (bool, entity.ChainStatus, error) {
	var clusterID int64
	if err := tx.QueryRow(ctx, `
		SELECT cluster_id FROM chain_participants
		WHERE chain_id = $1 AND request_id = $2
	`, chainID, requestID).Scan(&clusterID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, "", repository.MapDBErr(err)
		}
		return false, "", entity.ErrChainVoteForbidden
	}

	var hasOwnVote bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM votes WHERE chain_id = $1 AND request_id = $2
		)
	`, chainID, requestID).Scan(&hasOwnVote); err != nil {
		return false, "", fmt.Errorf("check declined participant vote: %w", err)
	}
	var isInvitedReplacement bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM chain_replacement_attempts
			WHERE chain_id = $1 AND request_id = $2 AND status = 'INVITED'
		)
	`, chainID, requestID).Scan(&isInvitedReplacement); err != nil {
		return false, "", fmt.Errorf("check replacement invitation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO chain_replacement_attempts (chain_id, request_id, status)
		VALUES ($1, $2, 'DECLINED')
		ON CONFLICT (chain_id, request_id) DO UPDATE
		SET status = 'DECLINED', updated_at = NOW()
	`, chainID, requestID); err != nil {
		return false, "", fmt.Errorf("record declined replacement participant: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM votes
		WHERE chain_id = $1 AND (request_id = $2 OR target_request_id = $2)
	`, chainID, requestID); err != nil {
		return false, "", repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE exchange_offers SET status = 'ACTIVE', updated_at = NOW() WHERE id = $1`, requestID); err != nil {
		return false, "", repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE items SET status = 'ACTIVE', updated_at = NOW()
		WHERE id = (SELECT offered_item_id FROM exchange_offers WHERE id = $1)
		  AND status = 'UNAVAILABLE'
	`, requestID); err != nil {
		return false, "", fmt.Errorf("release declined item: %w", err)
	}

	var replacementAvailable bool
	if fastReplacementEligible && (hasOwnVote || isInvitedReplacement) {
		eligibility := ReplacementEligibilitySQL("$3", "declined.position", "candidate_item", "previous_offer", "next_item", "$4", false)
		if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM cluster_members candidate_member
			JOIN exchange_offers candidate ON candidate.id = candidate_member.request_id
			JOIN items candidate_item ON candidate_item.id = candidate.offered_item_id
			JOIN chain_participants declined ON declined.chain_id = $3 AND declined.request_id = $2
			JOIN chains c ON c.id = declined.chain_id
			JOIN chain_participants previous_cp ON previous_cp.chain_id = c.id
			 AND previous_cp.position = (declined.position - 1 + c.length) % c.length
			JOIN exchange_offers previous_offer ON previous_offer.id = previous_cp.request_id
			JOIN chain_participants next_cp ON next_cp.chain_id = c.id
			 AND next_cp.position = (declined.position + 1) % c.length
			JOIN exchange_offers next_offer ON next_offer.id = next_cp.request_id
			JOIN items next_item ON next_item.id = next_offer.offered_item_id
			WHERE candidate_member.cluster_id = $1 AND candidate.id <> $2
			  AND `+eligibility+`
		)
		`, clusterID, requestID, chainID, r.matchingThreshold).Scan(&replacementAvailable); err != nil {
			return false, "", repository.MapDBErr(err)
		}
	}
	if replacementAvailable {
		return true, entity.ChainStatusProposed, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers AS eo
		SET status = 'ACTIVE', updated_at = NOW()
		WHERE eo.id IN (SELECT request_id FROM chain_participants WHERE chain_id = $1)
		  AND eo.status IN ('IN_PROPOSAL', 'LOCKED')
		  AND NOT EXISTS (
			SELECT 1 FROM chain_participants other_cp
			JOIN chains other_chain ON other_chain.id = other_cp.chain_id
			JOIN votes other_vote ON other_vote.chain_id = other_cp.chain_id
			 AND other_vote.request_id = other_cp.request_id AND other_vote.vote = 'approved'
			WHERE other_cp.request_id = eo.id AND other_cp.chain_id <> $1
			  AND other_chain.status IN ('PROPOSED', 'FROZEN', 'IN_PROGRESS')
		  )
	`, chainID); err != nil {
		return false, "", repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE items AS item
		SET status = 'ACTIVE', updated_at = NOW()
		WHERE item.id IN (
			SELECT offer.offered_item_id
			FROM chain_participants participant
			JOIN exchange_offers offer ON offer.id = participant.request_id
			WHERE participant.chain_id = $1
		) AND item.status = 'UNAVAILABLE'
	`, chainID); err != nil {
		return false, "", fmt.Errorf("release rolled back proposal items: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE votes
		SET vote = 'pending', voted_at = NOW()
		WHERE chain_id = $1 AND vote IN ('approved', 'thinking')
	`, chainID); err != nil {
		return false, "", repository.MapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chain_replacement_attempts WHERE chain_id = $1`, chainID); err != nil {
		return false, "", fmt.Errorf("clear replacement attempts after rollback: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE chains SET status = 'CANDIDATE', freeze_deadline_at = NULL,
		    invalid_reason = 'participant_cancelled', version = version + 1, updated_at = NOW()
		WHERE id = $1 AND status = 'PROPOSED'
	`, chainID); err != nil {
		return false, "", repository.MapDBErr(err)
	}
	return false, entity.ChainStatusCandidate, nil
}

// CountApprovedVoters возвращает число участников цепочки, подтвердивших участие.
func (r *Postgres) CountApprovedVoters(ctx context.Context, tx database.Tx, chainID int64) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(DISTINCT vote.request_id)
		FROM votes AS vote
		JOIN chain_participants AS source
		  ON source.chain_id = vote.chain_id AND source.request_id = vote.request_id
		JOIN chain_participants AS target
		  ON target.chain_id = vote.chain_id AND target.request_id = vote.target_request_id
		WHERE vote.chain_id = $1 AND vote.vote = 'approved'
	`, chainID).Scan(&count); err != nil {
		return 0, repository.MapDBErr(err)
	}
	return count, nil
}

func (r *Postgres) CountApprovedVotersExcept(ctx context.Context, tx database.Tx, chainID, requestID int64) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(DISTINCT vote.request_id)
		FROM votes AS vote
		JOIN chain_participants AS source
		  ON source.chain_id = vote.chain_id AND source.request_id = vote.request_id
		JOIN chain_participants AS target
		  ON target.chain_id = vote.chain_id AND target.request_id = vote.target_request_id
		WHERE vote.chain_id = $1 AND vote.request_id <> $2 AND vote.vote = 'approved'
	`, chainID, requestID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count approved voters except participant: %w", err)
	}
	return count, nil
}

// FindParticipantEdge finds the structural edge (request→target) owned by a
// participant. The edge exists even before a replacement user has voted.
func (r *Postgres) FindParticipantEdge(ctx context.Context, tx database.Tx, chainID int64, userID string) (int64, int64, error) {
	var requestID, targetID int64
	err := tx.QueryRow(ctx, `
		SELECT source_participant.request_id, target_participant.request_id
		FROM chain_participants AS source_participant
		JOIN chains AS chain ON chain.id = source_participant.chain_id
		JOIN exchange_offers AS source ON source.id = source_participant.request_id
		JOIN chain_participants AS target_participant
		  ON target_participant.chain_id = source_participant.chain_id
		 AND target_participant.position = (source_participant.position + 1) % chain.length
		WHERE source_participant.chain_id = $1
		  AND source.user_id = $2
		ORDER BY source_participant.position
		LIMIT 1
	`, chainID, userID).Scan(&requestID, &targetID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, repository.MapDBErr(err)
		}
		return 0, 0, entity.ErrChainVoteForbidden
	}
	return requestID, targetID, nil
}
