package chain

import (
	"context"
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/utils/ranker"
)

// LoadRankerContextForRequests реализует matching.RankerContextLoader.
func (s *Service) LoadRankerContextForRequests(
	ctx context.Context, tx database.Tx, requestIDs []int64,
) (ranker.ContextSnapshot, error) {
	if s.repository == nil {
		return ranker.ContextSnapshot{}, entity.ErrChainRepositoryNotConfigured
	}
	return s.repository.LoadRankerContextForRequests(ctx, tx, requestIDs)
}

func (s *Service) refreshScore(ctx context.Context, tx database.Tx, chainID int64, stage entity.ChainStatus, event ranker.StateEvent) error {
	if s.scorer == nil {
		return nil
	}
	cosines, reliability, sizes, err := s.repository.LoadScoreFeatures(ctx, tx, chainID)
	if err != nil {
		return err
	}
	voters := s.repository.CountPendingVoters
	if event == ranker.EventConfirm {
		voters = s.repository.CountApprovedVoters
	}
	approved, err := voters(ctx, tx, chainID)
	if err != nil {
		return err
	}
	snap, err := s.repository.LoadRankerContext(ctx, tx, chainID)
	if err != nil {
		return err
	}
	state := ranker.ApplyContext(ranker.ChainState{
		Count:                   len(cosines),
		Stage:                   scoreStage(stage),
		Event:                   event,
		EdgeCosines:             cosines,
		ParticipantReliability:  reliability,
		ParticipantClusterSizes: sizes,
		ApprovedVotes:           approved,
	}, snap, time.Now())
	ranker.LogSparseChainState(state)
	score, err := s.scorer.Score(state)
	if err != nil {
		return err
	}
	return s.repository.UpdateScore(ctx, tx, chainID, score)
}

func rotRightInt(values []int, n int) []int {
	if len(values) == 0 {
		return values
	}
	n = n % len(values)
	rotated := make([]int, 0, len(values))
	rotated = append(rotated, values[n:]...)
	rotated = append(rotated, values[:n]...)
	return rotated
}

func rotRightFloat(values []float64, n int) []float64 {
	if len(values) == 0 {
		return values
	}
	n = n % len(values)
	rotated := make([]float64, 0, len(values))
	rotated = append(rotated, values[n:]...)
	rotated = append(rotated, values[:n]...)
	return rotated
}

func scoreStage(status entity.ChainStatus) ranker.ChainStateStatus {
	switch status {
	case entity.ChainStatusProposed:
		return ranker.ChainStateProposed
	case entity.ChainStatusFrozen:
		return ranker.ChainStateFrozen
	case entity.ChainStatusInProgress:
		return ranker.ChainStateInProgress
	case entity.ChainStatusCompleted:
		return ranker.ChainStateCompleted
	case entity.ChainStatusBroken:
		return ranker.ChainStateBroken
	default:
		return ranker.ChainStateCandidate
	}
}
