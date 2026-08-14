package chain

import (
	"fmt"
	"strings"
)

type replacementEligibility struct {
	ChainID       string // $1 / $3 / c.id
	VacancyPos    string // v.position / $6 / declined.position
	CandidateItem string // item / candidate_item
	PreviousOffer string // previous_offer / actor_offer
	NextItem      string // next_item
	Threshold     string // $3 / $4 / $7
	// ExcludeAlreadyInChain добавляет NOT EXISTS по chain_participants.
	ExcludeAlreadyInChain bool
}

func ReplacementEligibilitySQL(
	chainID, vacancyPos, candidateItem, previousOffer, nextItem, threshold string,
	excludeAlreadyInChain bool,
) string {
	return replacementEligibility{
		ChainID:               chainID,
		VacancyPos:            vacancyPos,
		CandidateItem:         candidateItem,
		PreviousOffer:         previousOffer,
		NextItem:              nextItem,
		Threshold:             threshold,
		ExcludeAlreadyInChain: excludeAlreadyInChain,
	}.sql()
}

func (p replacementEligibility) sql() string {
	parts := []string{
		`candidate.status = 'ACTIVE'`,
		`NOT EXISTS (
				SELECT 1
				FROM exchange_offers live_offer
				WHERE live_offer.offered_item_id = candidate.offered_item_id
				  AND live_offer.id <> candidate.id
				  AND live_offer.status IN ('IN_PROPOSAL', 'LOCKED')
			)`,
		fmt.Sprintf(`NOT EXISTS (
				SELECT 1 FROM chain_replacement_attempts attempt
				WHERE attempt.chain_id = %s AND attempt.request_id = candidate.id
			)`, p.ChainID),
		fmt.Sprintf(`candidate.user_id <> ALL (
				SELECT occupied.user_id FROM chain_participants occupied_cp
				JOIN exchange_offers occupied ON occupied.id = occupied_cp.request_id
				WHERE occupied_cp.chain_id = %s AND occupied_cp.position <> %s
			)`, p.ChainID, p.VacancyPos),
		fmt.Sprintf(`%s.embedding IS NOT NULL AND %s.want_embedding IS NOT NULL`, p.CandidateItem, p.PreviousOffer),
		fmt.Sprintf(`candidate.want_embedding IS NOT NULL AND %s.embedding IS NOT NULL`, p.NextItem),
		fmt.Sprintf(`%s.category IS NOT DISTINCT FROM %s.wanted_category`, p.CandidateItem, p.PreviousOffer),
		fmt.Sprintf(`%s.category IS NOT DISTINCT FROM candidate.wanted_category`, p.NextItem),
		fmt.Sprintf(`1 - (%s.embedding <=> %s.want_embedding) >= %s`, p.CandidateItem, p.PreviousOffer, p.Threshold),
		fmt.Sprintf(`1 - (%s.embedding <=> candidate.want_embedding) >= %s`, p.NextItem, p.Threshold),
	}
	if p.ExcludeAlreadyInChain {
		parts = append(parts, fmt.Sprintf(`NOT EXISTS (
				SELECT 1 FROM chain_participants current
				WHERE current.chain_id = %s AND current.request_id = candidate.id
			)`, p.ChainID))
	}
	var b strings.Builder
	for i, part := range parts {
		if i == 0 {
			b.WriteString(part)
			continue
		}
		b.WriteString("\n\t\t  AND ")
		b.WriteString(part)
	}
	return b.String()
}
