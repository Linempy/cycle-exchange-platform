package chain

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
)

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
		return repository.MapDBErr(err)
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
		return repository.MapDBErr(err)
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
		return repository.MapDBErr(err)
	}

	query, arguments := participantsInsert(chainID, draft)
	if _, err := tx.Exec(ctx, query, arguments...); err != nil {
		return repository.MapDBErr(err)
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
