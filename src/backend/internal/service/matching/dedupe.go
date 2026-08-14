package matching

import (
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

// dedupeDraft — уникальность по набору ClusterID участников.
func dedupeDraft(drafts []entity.ChainDraft, candidate entity.ChainDraft) []entity.ChainDraft {
	for i := range drafts {
		if sameClusterPath(drafts[i], candidate) {
			return drafts // уже есть такой путь — не дублируем
		}
	}
	return append(drafts, candidate)
}

func sameClusterPath(left, right entity.ChainDraft) bool {
	if len(left.Participants) != len(right.Participants) {
		return false
	}
	for position := range left.Participants {
		if left.Participants[position].ClusterID != right.Participants[position].ClusterID {
			return false
		}
	}
	return true
}
