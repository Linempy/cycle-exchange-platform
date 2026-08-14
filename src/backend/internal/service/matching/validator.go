package matching

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

// CandidateValidator фильтрует сырых кандидатов pgvector:
// порог score, чужой owner, уникальный request_id.
type CandidateValidator struct {
	threshold float64
}

func NewCandidateValidator(threshold float64) *CandidateValidator {
	return &CandidateValidator{threshold: threshold}
}

func (v *CandidateValidator) Validate(_ context.Context, candidates []entity.Candidate, forUserID string) []entity.Candidate {
	seen := make(map[int64]struct{}, len(candidates))
	result := make([]entity.Candidate, 0, len(candidates))

	for _, c := range candidates {
		if c.Score < v.threshold {
			continue
		}
		if c.OwnerID == forUserID {
			continue
		}
		if _, dup := seen[c.RequestID]; dup {
			continue
		}
		seen[c.RequestID] = struct{}{}
		result = append(result, c)
	}
	return result
}
