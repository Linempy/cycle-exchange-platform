package entity

// ChainDraft — найденная, но ещё не сохранённая цепочка кластеров обмена.
// Порядок Participants задаёт позиции кластеров в цикле.
//
// Поля ClusterSizes / EdgeCosines / ParticipantReliability — сырые данные фич,
// которые CycleFinder собирает при построении цикла. Score цепочки НЕ считает
// CycleFinder: Ranker (ChainScoreCalculator) присваивает его один раз на фасаде
type ChainDraft struct {
	Participants           []ChainDraftParticipant
	ClusterSizes           []int
	EdgeCosines            []float64
	ParticipantReliability []float64
	Score                  float64
}

// ChainDraftParticipant описывает одну вершину-кластер.
// RequestID хранит заявку-представителя, через которую поиск пришёл в кластер.
// Идентичность вершины и цепочки определяется только по ClusterID.
type ChainDraftParticipant struct {
	ClusterID int64
	RequestID int64
}
