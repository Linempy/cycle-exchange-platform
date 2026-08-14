package chain

import (
	"context"
	"sort"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

// SaveCandidates проверяет и сохраняет кандидатные цепочки в переданной транзакции.
func (s *Service) SaveCandidates(ctx context.Context, tx database.Tx, drafts []entity.ChainDraft) error {
	if s.repository == nil {
		return entity.ErrChainRepositoryNotConfigured
	}

	canonical := make([]entity.ChainDraft, 0, len(drafts))
	for _, draft := range drafts {
		normalized, err := normalizeDraft(draft)
		if err != nil {
			return err
		}
		canonical = append(canonical, normalized)
	}
	sort.Slice(canonical, func(i, j int) bool {
		left := canonical[i].Participants
		right := canonical[j].Participants
		for position := 0; position < len(left) && position < len(right); position++ {
			if left[position].ClusterID != right[position].ClusterID {
				return left[position].ClusterID < right[position].ClusterID
			}
		}
		return len(left) < len(right)
	})
	return s.repository.SaveCandidates(ctx, tx, canonical)
}

func normalizeDraft(draft entity.ChainDraft) (entity.ChainDraft, error) {
	length := len(draft.Participants)
	if length < minClusters || length > maxClusters || draft.Score < 0 || draft.Score > 1 {
		return entity.ChainDraft{}, entity.ErrInvalidChainDraft
	}

	seenClusters := make(map[int64]struct{}, length)
	seenRequests := make(map[int64]struct{}, length)
	minimumPosition := 0
	for position, participant := range draft.Participants {
		if participant.ClusterID <= 0 || participant.RequestID <= 0 {
			return entity.ChainDraft{}, entity.ErrInvalidChainDraft
		}
		if _, exists := seenClusters[participant.ClusterID]; exists {
			return entity.ChainDraft{}, entity.ErrInvalidChainDraft
		}
		seenClusters[participant.ClusterID] = struct{}{}
		if _, exists := seenRequests[participant.RequestID]; exists {
			return entity.ChainDraft{}, entity.ErrInvalidChainDraft
		}
		seenRequests[participant.RequestID] = struct{}{}
		if participant.ClusterID < draft.Participants[minimumPosition].ClusterID {
			minimumPosition = position
		}
	}

	participants := make([]entity.ChainDraftParticipant, 0, length)
	participants = append(participants, draft.Participants[minimumPosition:]...)
	participants = append(participants, draft.Participants[:minimumPosition]...)
	return entity.ChainDraft{
		Participants:           participants,
		Score:                  draft.Score,
		ClusterSizes:           rotRightInt(draft.ClusterSizes, minimumPosition),
		EdgeCosines:            rotRightFloat(draft.EdgeCosines, minimumPosition),
		ParticipantReliability: rotRightFloat(draft.ParticipantReliability, minimumPosition),
	}, nil
}
