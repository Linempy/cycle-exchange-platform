package search

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
)

func (s *Search) FindOutgoingByThreshold(ctx context.Context, want []float32, excludeUserID string, threshold float64) ([]entity.Candidate, error) {
	rows, err := s.pool.Query(ctx, constQueryOutgoingThreshold, embedLiteral(want), excludeUserID, threshold)
	if err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	return collectCandidates(rows)
}

func (s *Search) FindIncomingByThreshold(ctx context.Context, mine []float32, excludeUserID string, threshold float64) ([]entity.Candidate, error) {
	rows, err := s.pool.Query(ctx, constQueryIncomingThreshold, embedLiteral(mine), excludeUserID, threshold)
	if err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	return collectCandidates(rows)
}

func (s *Search) FindOutgoingTopK(ctx context.Context, want []float32, excludeUserID string, k int) ([]entity.Candidate, error) {
	rows, err := s.pool.Query(ctx, constQueryOutgoingTopK, embedLiteral(want), excludeUserID, k)
	if err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	return collectCandidates(rows)
}

func (s *Search) FindIncomingTopK(ctx context.Context, mine []float32, excludeUserID string, k int) ([]entity.Candidate, error) {
	rows, err := s.pool.Query(ctx, constQueryIncomingTopK, embedLiteral(mine), excludeUserID, k)
	if err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	return collectCandidates(rows)
}

func (s *Search) FindSimilarOffers(
	ctx context.Context,
	offer string,
	want string,
	category string,
	wantedCategory string,
	excludeOfferID int64,
	threshold float64,
	directionMargin float64,
	k int,
) ([]entity.Candidate, error) {
	rows, err := s.pool.Query(
		ctx,
		querySimilarOffers,
		offer,
		want,
		excludeOfferID,
		category,
		wantedCategory,
		threshold,
		k,
		directionMargin,
	)
	if err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	return collectCandidates(rows)
}
