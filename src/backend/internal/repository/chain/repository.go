package chain

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	chainservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/chain"
)

// Postgres хранит цепочки и их участников в PostgreSQL.
type Postgres struct {
	pool              *pgxpool.Pool
	matchingThreshold float64
}

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

// NewRepository создаёт репозиторий цепочек.
func NewRepository(pool *pgxpool.Pool, thresholds ...float64) *Postgres {
	threshold := 0.5
	if len(thresholds) > 0 && thresholds[0] > 0 && thresholds[0] <= 1 {
		threshold = thresholds[0]
	}
	return &Postgres{pool: pool, matchingThreshold: threshold}
}

// SaveCandidates атомарно сохраняет цепочки и участников в уже открытой транзакции.
func (r *Postgres) SaveCandidates(ctx context.Context, tx database.Tx, drafts []entity.ChainDraft) error {
	for _, draft := range drafts {
		if err := saveCandidate(ctx, tx, draft); err != nil {
			return err
		}
	}
	return nil
}

func saveCandidate(ctx context.Context, tx database.Tx, draft entity.ChainDraft) error {
	draft = canonicalizeDraft(draft)
	clusterIDs := make([]int64, len(draft.Participants))
	for i, participant := range draft.Participants {
		clusterIDs[i] = participant.ClusterID
	}
	signature := chainSignature(clusterIDs)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, signature); err != nil {
		return fmt.Errorf("lock candidate chain signature: %w", err)
	}

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM chains AS c
			WHERE c.status = 'CANDIDATE'
			  AND c.length = $2
			  AND ARRAY(
				SELECT cp.cluster_id
				FROM chain_participants AS cp
				WHERE cp.chain_id = c.id
				ORDER BY cp.position
			  ) = $1::bigint[]
		)
	`, clusterIDs, len(clusterIDs)).Scan(&exists); err != nil {
		return fmt.Errorf("check candidate chain duplicate: %w", err)
	}
	if exists {
		return nil
	}

	var chainID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO chains (status, score, length)
		VALUES ('CANDIDATE', $1, $2)
		RETURNING id
	`, draft.Score, len(draft.Participants)).Scan(&chainID); err != nil {
		return fmt.Errorf("insert candidate chain: %w", err)
	}

	query, arguments := participantsInsert(chainID, draft)
	if _, err := tx.Exec(ctx, query, arguments...); err != nil {
		return fmt.Errorf("insert chain participants: %w", err)
	}
	return nil
}

func participantsInsert(chainID int64, draft entity.ChainDraft) (string, []any) {
	participants := draft.Participants
	values := make([]string, 0, len(participants))
	arguments := make([]any, 0, len(participants)*7)
	for position, participant := range participants {
		base := position*7 + 1
		values = append(values, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			base, base+1, base+2, base+3, base+4, base+5, base+6,
		))
		arguments = append(arguments,
			chainID,
			participant.ClusterID,
			participant.RequestID,
			position,
			edgeCosineAt(draft, position),
			reliabilityAt(draft, position),
			clusterSizeAt(draft, position),
		)
	}
	return `
		INSERT INTO chain_participants (
			chain_id, cluster_id, request_id, position,
			edge_cosine, reliability, cluster_size
		)
		VALUES ` + strings.Join(values, ", "), arguments
}

func edgeCosineAt(draft entity.ChainDraft, position int) float64 {
	if position < len(draft.EdgeCosines) {
		return draft.EdgeCosines[position]
	}
	return 0
}

func reliabilityAt(draft entity.ChainDraft, position int) float64 {
	if position < len(draft.ParticipantReliability) {
		return draft.ParticipantReliability[position]
	}
	return 0.75
}

func clusterSizeAt(draft entity.ChainDraft, position int) int {
	if position < len(draft.ClusterSizes) {
		return draft.ClusterSizes[position]
	}
	return 1
}

func chainSignature(clusterIDs []int64) string {
	clusterIDs = canonicalClusterCycle(clusterIDs)
	parts := make([]string, len(clusterIDs))
	for i, clusterID := range clusterIDs {
		parts[i] = strconv.FormatInt(clusterID, 10)
	}
	return strings.Join(parts, ":")
}

func canonicalizeDraft(draft entity.ChainDraft) entity.ChainDraft {
	if len(draft.Participants) < 2 {
		return draft
	}
	best := 0
	for i := 1; i < len(draft.Participants); i++ {
		if draft.Participants[i].ClusterID < draft.Participants[best].ClusterID {
			best = i
		}
	}
	rotateParticipants := append(append([]entity.ChainDraftParticipant(nil), draft.Participants[best:]...), draft.Participants[:best]...)
	draft.Participants = rotateParticipants
	draft.EdgeCosines = rotateFloat64(draft.EdgeCosines, best)
	draft.ParticipantReliability = rotateFloat64(draft.ParticipantReliability, best)
	draft.ClusterSizes = rotateInts(draft.ClusterSizes, best)
	return draft
}

func rotateFloat64(values []float64, start int) []float64 {
	if len(values) == 0 || start <= 0 || start >= len(values) {
		return values
	}
	return append(append([]float64(nil), values[start:]...), values[:start]...)
}

func rotateInts(values []int, start int) []int {
	if len(values) == 0 || start <= 0 || start >= len(values) {
		return values
	}
	return append(append([]int(nil), values[start:]...), values[:start]...)
}

func canonicalClusterCycle(clusterIDs []int64) []int64 {
	if len(clusterIDs) < 2 {
		return append([]int64(nil), clusterIDs...)
	}
	best := 0
	for start := 1; start < len(clusterIDs); start++ {
		for offset := 0; offset < len(clusterIDs); offset++ {
			left := clusterIDs[(start+offset)%len(clusterIDs)]
			right := clusterIDs[(best+offset)%len(clusterIDs)]
			if left < right {
				best = start
				break
			}
			if left > right {
				break
			}
		}
	}
	result := make([]int64, len(clusterIDs))
	for i := range result {
		result[i] = clusterIDs[(best+i)%len(clusterIDs)]
	}
	return result
}

// List возвращает актуальные цепочки пользователя без N+1-запросов.
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

// ListForOffer возвращает актуальные цепочки конкретной заявки её владельцу.
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
		return nil, fmt.Errorf("verify exchange offer owner: %w", err)
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

// LockForVote serializes all responses for one chain and returns its current state.
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
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, entity.ErrChainNotFound
	}
	if err != nil {
		return "", 0, fmt.Errorf("lock chain for vote: %w", err)
	}
	return status, length, nil
}

// ValidateVoteParticipants verifies ownership and the directed edge between
// adjacent chain positions while the chain row is locked by the caller.
func (r *Postgres) ValidateVoteParticipants(
	ctx context.Context,
	tx database.Tx,
	userID string,
	chainID, requestID, targetRequestID int64,
	chainLength int,
) error {
	var sourcePosition int
	err := tx.QueryRow(ctx, validateVoteSourceQuery, chainID, requestID, userID).Scan(&sourcePosition)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ErrChainVoteForbidden
	}
	if err != nil {
		return fmt.Errorf("validate vote source: %w", err)
	}

	var targetPosition int
	err = tx.QueryRow(ctx, validateVoteTargetQuery, chainID, targetRequestID).Scan(&targetPosition)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ErrInvalidVoteTarget
	}
	if err != nil {
		return fmt.Errorf("validate vote target: %w", err)
	}
	if chainLength <= 0 || targetPosition != (sourcePosition+1)%chainLength {
		return entity.ErrInvalidVoteTarget
	}
	return nil
}

// GetVote returns an existing response only when the source request belongs to
// the authenticated user. It is used to make retries idempotent after proposal.
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
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ChainVote{}, entity.ErrChainVoteForbidden
	}
	if err != nil {
		return entity.ChainVote{}, fmt.Errorf("get chain vote: %w", err)
	}
	return vote, nil
}

// UpsertPendingVote records a candidate response without creating duplicates.
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
		return time.Time{}, fmt.Errorf("upsert chain vote: %w", err)
	}
	return votedAt, nil
}

// DeletePendingVote withdraws only a candidate-stage response. A missing row
// is not an error, which makes retries idempotent.
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
		return fmt.Errorf("delete pending chain vote: %w", err)
	}
	return nil
}

// ListPendingVoteEdges returns candidate responses for the service's local DFS.
func (r *Postgres) ListPendingVoteEdges(
	ctx context.Context,
	tx database.Tx,
	chainID int64,
) ([]entity.VoteEdge, error) {
	rows, err := tx.Query(ctx, listPendingVoteEdgesQuery, chainID)
	if err != nil {
		return nil, fmt.Errorf("list pending vote edges: %w", err)
	}
	defer rows.Close()

	edges := make([]entity.VoteEdge, 0)
	for rows.Next() {
		var edge entity.VoteEdge
		if err := rows.Scan(&edge.RequestID, &edge.TargetRequestID, &edge.Position); err != nil {
			return nil, fmt.Errorf("scan pending vote edge: %w", err)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending vote edges: %w", err)
	}
	return edges, nil
}

// Propose pins the concrete requests selected by DFS, locks them into
// IN_PROPOSAL, and resets votes to pending while the chain is a candidate.
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
			return fmt.Errorf("pin chain participant at position %d: %w", position, err)
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
		return fmt.Errorf("propose chain: %w", err)
	}
	if result.RowsAffected() != 1 {
		return entity.ErrChainNotCandidate
	}

	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers
		SET status = 'IN_PROPOSAL', updated_at = NOW()
		WHERE id = ANY($1::bigint[])
		  AND status IN ('ACTIVE', 'IN_PROPOSAL')
	`, requestIDsByPosition); err != nil {
		return fmt.Errorf("lock proposed requests: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE votes
		SET vote = 'pending', voted_at = NOW()
		WHERE chain_id = $1
		  AND vote <> 'pending'
	`, chainID); err != nil {
		return fmt.Errorf("reset votes to pending: %w", err)
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
		UPDATE votes
		SET vote = 'pending', voted_at = NOW()
		WHERE chain_id = $1
		  AND vote IN ('approved', 'thinking')
	`, chainID); err != nil {
		return false, fmt.Errorf("reset expired proposal confirmations: %w", err)
	}
	return true, nil
}

func (r *Postgres) loadVisibleChains(ctx context.Context, userID string, chainID, offerID int64) ([]entity.Chain, error) {
	rows, err := r.pool.Query(ctx, loadVisibleChainsQuery, userID, chainID, offerID)
	if err != nil {
		return nil, fmt.Errorf("list visible chains: %w", err)
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
			return nil, fmt.Errorf("scan visible chain: %w", err)
		}
		chains = append(chains, chain)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate visible chains: %w", err)
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
		return fmt.Errorf("load chain participants: %w", err)
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
			return fmt.Errorf("scan chain participant: %w", err)
		}
		if chain := byID[participant.ChainID]; chain != nil {
			chain.Participants = append(chain.Participants, participant)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate chain participants: %w", err)
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
		return fmt.Errorf("load participant votes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var chainID, requestID, targetRequestID int64
		var vote entity.VoteValue
		if err := rows.Scan(&chainID, &requestID, &targetRequestID, &vote); err != nil {
			return fmt.Errorf("scan viewer vote: %w", err)
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
		return fmt.Errorf("iterate participant votes: %w", err)
	}
	return nil
}

// MarkRequestInProposal переводит откликнувшуюся заявку в мягкую блокировку.
func (r *Postgres) MarkRequestInProposal(ctx context.Context, tx database.Tx, requestID int64) error {
	if _, err := tx.Exec(ctx, `
		UPDATE exchange_offers
		SET status = 'IN_PROPOSAL', updated_at = NOW()
		WHERE id = $1 AND status IN ('ACTIVE', 'IN_PROPOSAL')
	`, requestID); err != nil {
		return fmt.Errorf("mark request in proposal: %w", err)
	}
	return nil
}

// RestoreActiveIfNoPendingVotes возвращает заявку в ACTIVE, когда у неё не
// осталось других pending-голосов как откликнувшейся.
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
		return fmt.Errorf("restore request active: %w", err)
	}
	return nil
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
		return nil, nil, nil, fmt.Errorf("load score features: %w", err)
	}
	defer rows.Close()

	var cosines []float64
	var reliability []float64
	var sizes []int
	for rows.Next() {
		var c, rel float64
		var size int
		if err := rows.Scan(&c, &rel, &size); err != nil {
			return nil, nil, nil, fmt.Errorf("scan score feature: %w", err)
		}
		cosines = append(cosines, c)
		reliability = append(reliability, rel)
		sizes = append(sizes, size)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("iterate score features: %w", err)
	}
	return cosines, reliability, sizes, nil
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
		return 0, fmt.Errorf("count pending voters: %w", err)
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
		return fmt.Errorf("update chain score: %w", err)
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
		return fmt.Errorf("confirm participant vote: %w", err)
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
		return fmt.Errorf("mark participant thinking: %w", err)
	}
	return nil
}

// DeclineParticipant removes the participant's confirmation and releases its
// request. A replacement is allowed only when every other participant has
// confirmed and the declining participant has an own vote in the chain. An
// invited replacement has no own vote yet; its refusal rolls the proposal back
// to CANDIDATE instead of opening an unsupported second replacement round.
func (r *Postgres) DeclineParticipant(ctx context.Context, tx database.Tx, chainID, requestID int64, fastReplacementEligible bool) (bool, entity.ChainStatus, error) {
	var openReplacements int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM chain_participants cp
		JOIN exchange_offers eo ON eo.id = cp.request_id
		WHERE cp.chain_id = $1 AND cp.request_id <> $2 AND eo.status = 'ACTIVE'
		  AND NOT EXISTS (SELECT 1 FROM votes v WHERE v.chain_id = cp.chain_id AND v.request_id = cp.request_id)
	`, chainID, requestID).Scan(&openReplacements); err != nil {
		return false, "", fmt.Errorf("count open replacements: %w", err)
	}
	if openReplacements > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE exchange_offers SET status = 'ACTIVE', updated_at = NOW()
			WHERE id IN (SELECT request_id FROM chain_participants WHERE chain_id = $1)
		`, chainID); err != nil {
			return false, "", fmt.Errorf("release aborted proposal requests: %w", err)
		}
		if err := r.DeleteChain(ctx, tx, chainID); err != nil {
			return false, "", err
		}
		return false, entity.ChainStatusBroken, nil
	}

	var clusterID int64
	if err := tx.QueryRow(ctx, `
		SELECT cluster_id FROM chain_participants
		WHERE chain_id = $1 AND request_id = $2
	`, chainID, requestID).Scan(&clusterID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", entity.ErrChainVoteForbidden
		}
		return false, "", fmt.Errorf("load declined participant cluster: %w", err)
	}

	var hasOwnVote bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM votes WHERE chain_id = $1 AND request_id = $2
		)
	`, chainID, requestID).Scan(&hasOwnVote); err != nil {
		return false, "", fmt.Errorf("check declined participant vote: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM votes WHERE chain_id = $1 AND request_id = $2`, chainID, requestID); err != nil {
		return false, "", fmt.Errorf("delete declined participant vote: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE exchange_offers SET status = 'ACTIVE', updated_at = NOW() WHERE id = $1`, requestID); err != nil {
		return false, "", fmt.Errorf("release declined request: %w", err)
	}

	var replacementAvailable bool
	if fastReplacementEligible && hasOwnVote {
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
			  AND candidate.status IN ('ACTIVE', 'IN_PROPOSAL')
			  AND candidate.user_id <> ALL (
				SELECT occupied.user_id FROM chain_participants occupied_cp
				JOIN exchange_offers occupied ON occupied.id = occupied_cp.request_id
				WHERE occupied_cp.chain_id = c.id AND occupied_cp.position <> declined.position
			  )
			  AND candidate_item.embedding IS NOT NULL AND previous_offer.want_embedding IS NOT NULL
			  AND candidate.want_embedding IS NOT NULL AND next_item.embedding IS NOT NULL
			  AND candidate_item.category IS NOT DISTINCT FROM previous_offer.wanted_category
			  AND next_item.category IS NOT DISTINCT FROM candidate.wanted_category
			  AND 1 - (candidate_item.embedding <=> previous_offer.want_embedding) >= $4
			  AND 1 - (next_item.embedding <=> candidate.want_embedding) >= $4
		)
		`, clusterID, requestID, chainID, r.matchingThreshold).Scan(&replacementAvailable); err != nil {
			return false, "", fmt.Errorf("check fast replacement: %w", err)
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
		return false, "", fmt.Errorf("rollback proposed requests: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE votes
		SET vote = 'pending', voted_at = NOW()
		WHERE chain_id = $1 AND vote IN ('approved', 'thinking')
	`, chainID); err != nil {
		return false, "", fmt.Errorf("reset proposal confirmations: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE chains SET status = 'CANDIDATE', version = version + 1, updated_at = NOW()
		WHERE id = $1 AND status = 'PROPOSED'
	`, chainID); err != nil {
		return false, "", fmt.Errorf("rollback proposed chain: %w", err)
	}
	return false, entity.ChainStatusCandidate, nil
}

// ListReplacementOptions returns active requests from the declined position's
// cluster. Only the previous participant, whose receive option disappeared,
// may list them.
func (r *Postgres) ListReplacementOptions(ctx context.Context, userID string, chainID int64) ([]entity.ReplacementOption, error) {
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
		WHERE candidate.status IN ('ACTIVE', 'IN_PROPOSAL')
		  AND candidate.user_id <> ALL (
			SELECT occupied.user_id FROM chain_participants occupied_cp
			JOIN exchange_offers occupied ON occupied.id = occupied_cp.request_id
			WHERE occupied_cp.chain_id = $1 AND occupied_cp.position <> v.position
		  )
		  AND item.embedding IS NOT NULL AND previous_offer.want_embedding IS NOT NULL
		  AND candidate.want_embedding IS NOT NULL AND next_item.embedding IS NOT NULL
		  AND item.category IS NOT DISTINCT FROM previous_offer.wanted_category
		  AND next_item.category IS NOT DISTINCT FROM candidate.wanted_category
		  AND 1 - (item.embedding <=> previous_offer.want_embedding) >= $3
		  AND 1 - (next_item.embedding <=> candidate.want_embedding) >= $3
		  AND NOT EXISTS (
			  SELECT 1 FROM chain_participants current
			  WHERE current.chain_id = $1 AND current.request_id = candidate.id
		  )
		ORDER BY COALESCE(position.reliability, 0.75) DESC, candidate.updated_at, candidate.id
	`, chainID, userID, r.matchingThreshold)
	if err != nil {
		return nil, fmt.Errorf("list replacement options: %w", err)
	}
	defer rows.Close()
	options := make([]entity.ReplacementOption, 0)
	for rows.Next() {
		var option entity.ReplacementOption
		if err := rows.Scan(&option.RequestID, &option.OfferedItemID, &option.Title, &option.Description,
			&option.WantedDescription, &option.ImageURL, &option.Reliability, &option.RespondedAt); err != nil {
			return nil, fmt.Errorf("scan replacement option: %w", err)
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replacement options: %w", err)
	}
	return options, nil
}

// SelectReplacement atomically pins an alternative request and transfers the
// previous participant's approved edge to it. The replacement does not receive
// a vote automatically: its owner must explicitly confirm or think.
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
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ErrChainVoteForbidden
	}
	if err != nil {
		return fmt.Errorf("load replacement context: %w", err)
	}
	_ = length

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
			  AND candidate.status IN ('ACTIVE', 'IN_PROPOSAL')
			  AND candidate.user_id <> ALL (
				SELECT occupied.user_id FROM chain_participants occupied_cp
				JOIN exchange_offers occupied ON occupied.id = occupied_cp.request_id
				WHERE occupied_cp.chain_id = $1 AND occupied_cp.position <> $6
			  )
			  AND candidate_item.embedding IS NOT NULL AND actor_offer.want_embedding IS NOT NULL
			  AND candidate.want_embedding IS NOT NULL AND next_item.embedding IS NOT NULL
			  AND candidate_item.category IS NOT DISTINCT FROM actor_offer.wanted_category
			  AND next_item.category IS NOT DISTINCT FROM candidate.wanted_category
			  AND 1 - (candidate_item.embedding <=> actor_offer.want_embedding) >= $7
			  AND 1 - (next_item.embedding <=> candidate.want_embedding) >= $7
			  AND NOT EXISTS (
				  SELECT 1 FROM chain_participants current
				  WHERE current.chain_id = $1 AND current.request_id = candidate.id
			  )
		)
	`, chainID, oldRequestID, replacementRequestID, actorRequestID, nextRequestID, position, r.matchingThreshold).Scan(&valid); err != nil {
		return fmt.Errorf("validate replacement request: %w", err)
	}
	if !valid {
		return entity.ErrInvalidVoteTarget
	}

	if _, err := tx.Exec(ctx, `DELETE FROM votes WHERE chain_id = $1 AND request_id = $2`, chainID, actorRequestID); err != nil {
		return fmt.Errorf("remove previous replacement edge: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO votes (chain_id, request_id, target_request_id, vote, voted_at)
		VALUES ($1, $2, $3, 'approved', NOW())
	`, chainID, actorRequestID, replacementRequestID); err != nil {
		return fmt.Errorf("transfer approved replacement edge: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE chain_participants SET request_id = $3 WHERE chain_id = $1 AND position = $2`, chainID, position, replacementRequestID); err != nil {
		return fmt.Errorf("pin replacement participant: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE exchange_offers SET status = 'IN_PROPOSAL', updated_at = NOW() WHERE id = $1`, replacementRequestID); err != nil {
		return fmt.Errorf("lock replacement request: %w", err)
	}
	return nil
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
		return 0, fmt.Errorf("count approved voters: %w", err)
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

// MarkRequestLocked переводит заявку в жёсткую блокировку.
func (r *Postgres) MarkRequestLocked(ctx context.Context, tx database.Tx, requestID int64) error {
	result, err := tx.Exec(ctx, `
		UPDATE exchange_offers
		SET status = 'LOCKED', updated_at = NOW()
		WHERE id = $1 AND status IN ('IN_PROPOSAL', 'ACTIVE', 'LOCKED')
	`, requestID)
	if err != nil {
		return fmt.Errorf("mark request locked: %w", err)
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
		    version = version + 1,
		    updated_at = NOW()
		WHERE id = $1 AND status = 'PROPOSED'
	`, chainID, deadline)
	if err != nil {
		return fmt.Errorf("freeze chain: %w", err)
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
		return fmt.Errorf("lock requests in chain: %w", err)
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
		return fmt.Errorf("mark items unavailable: %w", err)
	}
	return nil
}

// LoadChainRequestIDs возвращает заявки участников цепочки.
func (r *Postgres) LoadChainRequestIDs(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT cp.request_id
		FROM chain_participants AS cp
		WHERE cp.chain_id = $1
		ORDER BY cp.position
	`, chainID)
	if err != nil {
		return nil, fmt.Errorf("load chain request ids: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan chain request id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chain request ids: %w", err)
	}
	return ids, nil
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
		return fmt.Errorf("lock requests for freeze: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var requestID int64
		if err := rows.Scan(&requestID); err != nil {
			return fmt.Errorf("scan locked request: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate locked requests: %w", err)
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
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ChainStatusCandidate, nil
	}
	if err != nil {
		return entity.ChainStatus(""), fmt.Errorf("load request live chain status: %w", err)
	}
	return status, nil
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
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, entity.ErrChainVoteForbidden
	}
	if err != nil {
		return 0, 0, fmt.Errorf("find participant edge: %w", err)
	}
	return requestID, targetID, nil
}

// MarkRequestInProgress records an external handoff for a request pinned in a
// frozen chain. Repeated callbacks leave an already started or completed
// request unchanged.
func (r *Postgres) MarkRequestInProgress(
	ctx context.Context,
	tx database.Tx,
	chainID, requestID int64,
) (entity.RequestStatus, error) {
	var status entity.RequestStatus
	err := tx.QueryRow(ctx, `
		SELECT eo.status
		FROM chain_participants AS cp
		JOIN exchange_offers AS eo ON eo.id = cp.request_id
		WHERE cp.chain_id = $1 AND cp.request_id = $2
		FOR UPDATE OF eo
	`, chainID, requestID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", entity.ErrHandoffRequestInvalid
	}
	if err != nil {
		return "", fmt.Errorf("lock handoff request: %w", err)
	}

	switch status {
	case entity.RequestStatusLocked:
		if _, err := tx.Exec(ctx, `
			UPDATE exchange_offers
			SET status = 'IN_PROGRESS', updated_at = NOW()
			WHERE id = $1
		`, requestID); err != nil {
			return "", fmt.Errorf("mark request in progress: %w", err)
		}
		return entity.RequestStatusInProgress, nil
	case entity.RequestStatusInProgress, entity.RequestStatusDone:
		return status, nil
	default:
		return "", entity.ErrHandoffRequestInvalid
	}
}

// StartChain promotes a frozen chain after its first confirmed handoff.
func (r *Postgres) StartChain(ctx context.Context, tx database.Tx, chainID int64) error {
	result, err := tx.Exec(ctx, `
		UPDATE chains
		SET status = 'IN_PROGRESS', version = version + 1, updated_at = NOW()
		WHERE id = $1 AND status = 'FROZEN'
	`, chainID)
	if err != nil {
		return fmt.Errorf("start chain fulfillment: %w", err)
	}
	if result.RowsAffected() != 1 {
		return entity.ErrChainNotReadyForHandoff
	}
	return nil
}

// FindReceiptRequestStatus verifies that userID is the physical recipient of
// requestID. A participant gives its item to the previous chain position.
func (r *Postgres) FindReceiptRequestStatus(
	ctx context.Context,
	tx database.Tx,
	chainID, requestID int64,
	userID string,
) (entity.RequestStatus, error) {
	var status entity.RequestStatus
	err := tx.QueryRow(ctx, `
		SELECT source_offer.status
		FROM chain_participants AS source
		JOIN chains AS chain ON chain.id = source.chain_id
		JOIN exchange_offers AS source_offer ON source_offer.id = source.request_id
		JOIN chain_participants AS recipient
		  ON recipient.chain_id = source.chain_id
		 AND recipient.position = (source.position - 1 + chain.length) % chain.length
		JOIN exchange_offers AS recipient_offer ON recipient_offer.id = recipient.request_id
		WHERE source.chain_id = $1
		  AND source.request_id = $2
		  AND recipient_offer.user_id = $3
		FOR UPDATE OF source_offer
	`, chainID, requestID, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", entity.ErrChainReceiptForbidden
	}
	if err != nil {
		return "", fmt.Errorf("find receipt request: %w", err)
	}
	return status, nil
}

// MarkRequestDone closes a handed-off request after its recipient confirms
// receipt. Retrying a completed acknowledgement is intentionally successful.
func (r *Postgres) MarkRequestDone(ctx context.Context, tx database.Tx, requestID int64) error {
	result, err := tx.Exec(ctx, `
		UPDATE exchange_offers
		SET status = 'DONE', updated_at = NOW()
		WHERE id = $1 AND status = 'IN_PROGRESS'
	`, requestID)
	if err != nil {
		return fmt.Errorf("mark request done: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}

	var status entity.RequestStatus
	if err := tx.QueryRow(ctx, `SELECT status FROM exchange_offers WHERE id = $1`, requestID).Scan(&status); err != nil {
		return fmt.Errorf("load completed request status: %w", err)
	}
	if status == entity.RequestStatusDone {
		return nil
	}
	return entity.ErrChainHandoffPending
}

// AllChainRequestsDone reports whether every pinned request has been received.
func (r *Postgres) AllChainRequestsDone(ctx context.Context, tx database.Tx, chainID int64) (bool, error) {
	var complete bool
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) > 0
		   AND COUNT(*) FILTER (WHERE eo.status = 'DONE') = COUNT(*)
		FROM chain_participants AS cp
		JOIN exchange_offers AS eo ON eo.id = cp.request_id
		WHERE cp.chain_id = $1
	`, chainID).Scan(&complete)
	if err != nil {
		return false, fmt.Errorf("check completed chain requests: %w", err)
	}
	return complete, nil
}

// CompleteChain finalizes the aggregate state only after all pinned requests
// have been received.
func (r *Postgres) CompleteChain(ctx context.Context, tx database.Tx, chainID int64) error {
	result, err := tx.Exec(ctx, `
		UPDATE chains
		SET status = 'COMPLETED', version = version + 1, updated_at = NOW()
		WHERE id = $1 AND status = 'IN_PROGRESS'
	`, chainID)
	if err != nil {
		return fmt.Errorf("complete chain: %w", err)
	}
	if result.RowsAffected() != 1 {
		return entity.ErrChainNotReadyForHandoff
	}
	return nil
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
		return nil, fmt.Errorf("list chains containing request: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan affected chain id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate affected chain ids: %w", err)
	}
	return ids, nil
}

// DeleteChain удаляет цепочку целиком каскадом: голоса, участники, саму цепочку.
func (r *Postgres) DeleteChain(ctx context.Context, tx database.Tx, chainID int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM votes WHERE chain_id = $1`, chainID); err != nil {
		return fmt.Errorf("delete chain votes: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chain_participants WHERE chain_id = $1`, chainID); err != nil {
		return fmt.Errorf("delete chain participants: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chains WHERE id = $1`, chainID); err != nil {
		return fmt.Errorf("delete chain: %w", err)
	}
	return nil
}

// DeleteRequestParticipation удаляет только участие заявки в цепочках и её голоса,
// не трогая сами цепочки (их последующей пересборкой занимается matcher).
func (r *Postgres) DeleteRequestParticipation(ctx context.Context, tx database.Tx, requestID int64) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM votes WHERE request_id = $1 OR target_request_id = $1
	`, requestID); err != nil {
		return fmt.Errorf("delete request votes: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM chain_participants WHERE request_id = $1
	`, requestID); err != nil {
		return fmt.Errorf("delete request chain participation: %w", err)
	}
	return nil
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
		return nil, fmt.Errorf("list unselected chain requests: %w", err)
	}
	released := make([]int64, 0)
	for rows.Next() {
		var requestID int64
		if err := rows.Scan(&requestID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan unselected chain request: %w", err)
		}
		released = append(released, requestID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate unselected chain requests: %w", err)
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
		return nil, fmt.Errorf("delete unselected chain votes: %w", err)
	}

	for _, requestID := range released {
		if err := r.RestoreActiveIfNoPendingVotes(ctx, tx, requestID); err != nil {
			return nil, err
		}
	}
	return released, nil
}

func (r *Postgres) ReleaseCompetitorsFromOtherChains(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error) {
	// Удаляем голоса замороженных участников в других цепочках.
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
		return nil, fmt.Errorf("delete competitor votes: %w", err)
	}

	// Запоминаем, какие конкурирующие цепочки затронуты (до удаления участников).
	affected, err := func() ([]int64, error) {
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT cp_outside.chain_id
			FROM chain_participants AS cp_outside
			JOIN chain_participants AS frozen
			  ON frozen.chain_id = $1 AND frozen.request_id = cp_outside.request_id
			WHERE cp_outside.chain_id <> $1
		`, chainID)
		if err != nil {
			return nil, fmt.Errorf("list affected competitor chains: %w", err)
		}
		defer rows.Close()
		ids := make([]int64, 0)
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, fmt.Errorf("scan affected chain id: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate affected chain ids: %w", err)
		}
		return ids, nil
	}()
	if err != nil {
		return nil, err
	}

	// Убираем вхождения замороженных участников из конкурирующих цепочек.
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
		return nil, fmt.Errorf("remove frozen participants from competitor chains: %w", err)
	}

	return affected, nil
}

var _ chainservice.Repository = (*Postgres)(nil)
