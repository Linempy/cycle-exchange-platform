package chain

import (
	"context"
	"sort"
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/utils/ranker"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/validator"
)

func (s *Service) Vote(ctx context.Context, userID string, chainID int64, input VoteInput) (entity.ChainVote, error) {
	if s.repository == nil || s.transactions == nil {
		return entity.ChainVote{}, entity.ErrChainRepositoryNotConfigured
	}
	if chainID <= 0 {
		return entity.ChainVote{}, entity.ErrInvalidVoteTarget
	}
	if err := validator.Validate(&input); err != nil {
		return entity.ChainVote{}, err
	}
	if err := s.expireDue(ctx); err != nil {
		return entity.ChainVote{}, err
	}
	result := entity.ChainVote{
		ChainID:         chainID,
		RequestID:       input.RequestID,
		TargetRequestID: input.TargetRequestID,
		Vote:            entity.VotePending,
	}
	err := s.transactions.WithinTransaction(ctx, func(tx database.Tx) error {
		status, length, err := s.repository.LockForVote(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if status != entity.ChainStatusCandidate {
			existing, getErr := s.repository.GetVote(ctx, tx, userID, chainID, input.RequestID, input.TargetRequestID)
			if getErr == nil && existing.Vote == entity.VotePending {
				result = existing
				result.ChainStatus = status
				return nil
			}
			return entity.ErrChainNotCandidate
		}
		if err := s.repository.ValidateVoteParticipants(
			ctx, tx, userID, chainID, input.RequestID, input.TargetRequestID, length,
		); err != nil {
			return err
		}

		votedAt, err := s.repository.UpsertPendingVote(
			ctx, tx, chainID, input.RequestID, input.TargetRequestID,
		)
		if err != nil {
			return err
		}
		result.VotedAt = votedAt

		if err := s.repository.MarkRequestInProposal(ctx, tx, input.RequestID); err != nil {
			return err
		}
		result.ChainStatus = status

		edges, err := s.repository.ListPendingVoteEdges(ctx, tx, chainID)
		if err != nil {
			return err
		}
		cycle := findPendingCycle(length, edges)
		if len(cycle) == 0 {
			return s.refreshScore(ctx, tx, chainID, entity.ChainStatusCandidate, ranker.EventRespond)
		}
		if err := s.repository.Propose(ctx, tx, chainID, cycle, time.Now().Add(confirmationTTL)); err != nil {
			return err
		}
		result.ChainStatus = entity.ChainStatusProposed
		return s.refreshScore(ctx, tx, chainID, entity.ChainStatusProposed, ranker.EventRespond)
	})
	if err != nil {
		return entity.ChainVote{}, err
	}
	return result, nil
}

// WithdrawVote removes a primary response while the chain is still a candidate.
// Deleting an already absent response is intentionally successful.

func (s *Service) WithdrawVote(ctx context.Context, userID string, chainID int64, input VoteInput) error {
	if s.repository == nil || s.transactions == nil {
		return entity.ErrChainRepositoryNotConfigured
	}
	if chainID <= 0 {
		return entity.ErrInvalidVoteTarget
	}
	if err := validator.Validate(&input); err != nil {
		return err
	}
	if err := s.expireDue(ctx); err != nil {
		return err
	}

	return s.transactions.WithinTransaction(ctx, func(tx database.Tx) error {
		status, length, err := s.repository.LockForVote(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if status != entity.ChainStatusCandidate {
			return entity.ErrChainNotCandidate
		}
		if err := s.repository.ValidateVoteParticipants(
			ctx, tx, userID, chainID, input.RequestID, input.TargetRequestID, length,
		); err != nil {
			return err
		}
		if err := s.repository.DeletePendingVote(ctx, tx, chainID, input.RequestID, input.TargetRequestID); err != nil {
			return err
		}
		if err := s.repository.RestoreActiveIfNoPendingVotes(ctx, tx, input.RequestID); err != nil {
			return err
		}
		return s.refreshScore(ctx, tx, chainID, entity.ChainStatusCandidate, ranker.EventDecline)
	})
}

func findPendingCycle(length int, edges []entity.VoteEdge) []int64 {
	if length < minClusters || length > maxClusters {
		return nil
	}

	bySource := make(map[int64][]int64)
	starts := make([]int64, 0)
	for _, edge := range edges {
		bySource[edge.RequestID] = append(bySource[edge.RequestID], edge.TargetRequestID)
		if edge.Position == 0 {
			starts = append(starts, edge.RequestID)
		}
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	for source := range bySource {
		sort.Slice(bySource[source], func(i, j int) bool {
			return bySource[source][i] < bySource[source][j]
		})
	}

	for _, start := range starts {
		path := []int64{start}
		if findCyclePath(start, start, length, bySource, &path) {
			return path
		}
	}
	return nil
}

func findCyclePath(start, current int64, length int, bySource map[int64][]int64, path *[]int64) bool {
	if len(*path) == length {
		for _, target := range bySource[current] {
			if target == start {
				return true
			}
		}
		return false
	}

	for _, target := range bySource[current] {
		if containsRequest(*path, target) {
			continue
		}
		*path = append(*path, target)
		if findCyclePath(start, target, length, bySource, path) {
			return true
		}
		*path = (*path)[:len(*path)-1]
	}
	return false
}

func containsRequest(requestIDs []int64, target int64) bool {
	for _, requestID := range requestIDs {
		if requestID == target {
			return true
		}
	}
	return false
}
