package search

import (
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
	"github.com/jackc/pgx/v5"
)

func collectCandidates(rows pgx.Rows) ([]entity.Candidate, error) {
	defer rows.Close()

	candidates := make([]entity.Candidate, 0)
	for rows.Next() {
		var c entity.Candidate
		if err := rows.Scan(&c.RequestID, &c.ItemID, &c.OwnerID, &c.Score); err != nil {
			if mappedErr, ok := repository.DBErrToErr(err); ok {
				return nil, mappedErr
			}
			return nil, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	return candidates, nil
}

func collectCandidateEdges(rows pgx.Rows) ([]entity.CandidateEdge, error) {
	defer rows.Close()

	edges := make([]entity.CandidateEdge, 0)
	for rows.Next() {
		var edge entity.CandidateEdge
		if err := rows.Scan(
			&edge.FromRequestID,
			&edge.FromClusterID,
			&edge.FromOwnerID,
			&edge.ToRequestID,
			&edge.ToClusterID,
			&edge.ToOwnerID,
			&edge.Score,
		); err != nil {
			if mappedErr, ok := repository.DBErrToErr(err); ok {
				return nil, mappedErr
			}
			return nil, err
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		if mappedErr, ok := repository.DBErrToErr(err); ok {
			return nil, mappedErr
		}
		return nil, err
	}
	return edges, nil
}
