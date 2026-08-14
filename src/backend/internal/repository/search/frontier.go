package search

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
	"github.com/jackc/pgx/v5"
)

func (s *Search) LoadOutgoingFrontier(
	ctx context.Context,
	tx pgx.Tx,
	requestIDs []int64,
	k int,
	threshold float64,
) ([]entity.CandidateEdge, error) {
	if len(requestIDs) == 0 {
		return []entity.CandidateEdge{}, nil
	}

	rows, err := tx.Query(ctx, queryOutgoingFrontier, requestIDs, k, threshold)
	if err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	return collectCandidateEdges(rows)
}

func (s *Search) LoadIncomingToStart(
	ctx context.Context,
	tx pgx.Tx,
	startRequestID int64,
	k int,
	threshold float64,
) ([]entity.CandidateEdge, error) {
	rows, err := tx.Query(ctx, queryIncomingToStart, startRequestID, k, threshold)
	if err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	return collectCandidateEdges(rows)
}
