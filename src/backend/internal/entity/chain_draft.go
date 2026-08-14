package entity

// ChainDraft — ещё не сохранённая цепочка; порядок Participants = позиции в цикле.
// ClusterSizes/EdgeCosines/ParticipantReliability — сырые фичи; Score ставит Ranker на фасаде.
type ChainDraft struct {
	Participants           []ChainDraftParticipant
	ClusterSizes           []int
	EdgeCosines            []float64
	ParticipantReliability []float64
	Score                  float64
}

// RequestID — представитель входа в кластер; идентичность вершины — ClusterID.
type ChainDraftParticipant struct {
	ClusterID int64
	RequestID int64
}
