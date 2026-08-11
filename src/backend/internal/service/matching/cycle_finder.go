package matching

import (
	"context"
	"sort"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

const (
	minCycleLength        = 2
	maxCycleLength        = 5
	defaultOutgoingK      = 20
	defaultCycleThreshold = 0.5
	defaultDraftCapHint   = 64
)

// FrontierLoader описывает пакетные PostgreSQL-запросы, нужные CycleFinder.
// Интерфейс объявлен здесь, у потребителя; repository/search предоставляет реализацию.
type FrontierLoader interface {
	LoadOutgoingFrontier(
		ctx context.Context,
		tx database.Tx,
		requestIDs []int64,
		k int,
		threshold float64,
	) ([]entity.CandidateEdge, error)
	LoadIncomingToStart(
		ctx context.Context,
		tx database.Tx,
		startRequestID int64,
		k int,
		threshold float64,
	) ([]entity.CandidateEdge, error)
}

// CycleFinder строит локальный граф вокруг одной заявки и ищет в нём простые циклы.
type CycleFinder struct {
	loader      FrontierLoader
	outgoingK   int
	maxDrafts   int
	threshold   float64
	minAverage  float64
	maxScoreGap float64
}

type cycleVertex struct {
	clusterID int64
	requestID int64
	ownerID   string
}

// NewCycleFinder создаёт поиск циклов с безопасными значениями по умолчанию.
func NewCycleFinder(loader FrontierLoader, outgoingK, capHint int, threshold float64) *CycleFinder {
	if outgoingK <= 0 {
		outgoingK = defaultOutgoingK
	}
	if capHint <= 0 {
		capHint = defaultDraftCapHint
	}
	if threshold <= 0 || threshold > 1 {
		threshold = defaultCycleThreshold
	}
	return &CycleFinder{
		loader:      loader,
		outgoingK:   outgoingK,
		maxDrafts:   capHint,
		threshold:   threshold,
		minAverage:  threshold,
		maxScoreGap: 1,
	}
}

// WithQualityRules добавляет проверку качества уже собранного цикла. Порог
// каждой стрелки остаётся в NewCycleFinder, а эти правила защищают от циклов с
// низким средним качеством или слишком неравномерными направлениями обмена.
func (f *CycleFinder) WithQualityRules(minAverage, maxScoreGap float64) *CycleFinder {
	if minAverage > 0 && minAverage <= 1 {
		f.minAverage = minAverage
	}
	if maxScoreGap >= 0 && maxScoreGap <= 1 {
		f.maxScoreGap = maxScoreGap
	}
	return f
}

// Find возвращает ВСЕ уникальные простые циклы длиной 2–5, начинающиеся со
// startRequestID. Отбор по score тут не выполняется (это дело Ranker на фасаде),
// поэтому возвращаются все найденные цепочки, без обрезки «топ-N».
// Отсутствие подходящих циклов является штатным результатом, а не ошибкой.
func (f *CycleFinder) Find(ctx context.Context, tx database.Tx, startRequestID int64) ([]entity.ChainDraft, error) {
	if f.loader == nil || startRequestID <= 0 {
		return []entity.ChainDraft{}, nil
	}

	closingEdges, err := f.loader.LoadIncomingToStart(
		ctx,
		tx,
		startRequestID,
		f.outgoingK,
		f.threshold,
	)
	if err != nil {
		return nil, err
	}

	closers, startClusterID, startOwnerID := f.indexClosers(startRequestID, closingEdges)
	if len(closers) == 0 || startClusterID == 0 {
		return []entity.ChainDraft{}, nil
	}

	adjacency, err := f.loadLocalGraph(ctx, tx, startRequestID)
	if err != nil {
		return nil, err
	}
	if len(adjacency[startRequestID]) == 0 {
		return []entity.ChainDraft{}, nil
	}

	path := []cycleVertex{{
		clusterID: startClusterID,
		requestID: startRequestID,
		ownerID:   startOwnerID,
	}}
	visitedClusters := map[int64]bool{startClusterID: true}
	visitedOwners := make(map[string]bool)
	if startOwnerID != "" {
		visitedOwners[startOwnerID] = true
	}
	drafts := make([]entity.ChainDraft, 0, f.maxDrafts)

	var dfs func(currentRequestID int64, cosines []float64)
	dfs = func(currentRequestID int64, cosines []float64) {
		if len(drafts) >= f.maxDrafts {
			return
		}
		if len(path) >= minCycleLength {
			if closing, ok := closers[currentRequestID]; ok {
				edgeCosines := append(append([]float64(nil), cosines...), closing.Score)
				if f.acceptsQuality(edgeCosines) {
					participants := make([]entity.ChainDraftParticipant, len(path))
					for position, vertex := range path {
						participants[position] = entity.ChainDraftParticipant{
							ClusterID: vertex.clusterID,
							RequestID: vertex.requestID,
						}
					}

					sizes := make([]int, len(participants))
					reliability := make([]float64, len(participants))
					for i := range participants {
						sizes[i] = 1          // MVP: размер кластера; позже — из ClusterSynchronizer/разметки
						reliability[i] = 0.75 // MVP: константная надёжность (нет личного рейтинга)
					}

					draft := entity.ChainDraft{
						Participants:           participants,
						ClusterSizes:           sizes,
						EdgeCosines:            edgeCosines,
						ParticipantReliability: reliability,
						// Score НЕ заполняем — его назначает Ranker на фасаде.
					}
					drafts = dedupeDraft(drafts, draft)
				}
			}
		}

		if len(path) == maxCycleLength {
			return
		}

		for _, edge := range adjacency[currentRequestID] {
			if len(drafts) >= f.maxDrafts {
				return
			}
			if edge.FromRequestID != currentRequestID ||
				edge.ToRequestID == startRequestID ||
				visitedClusters[edge.ToClusterID] ||
				(edge.ToOwnerID != "" && visitedOwners[edge.ToOwnerID]) {
				continue
			}

			visitedClusters[edge.ToClusterID] = true
			if edge.ToOwnerID != "" {
				visitedOwners[edge.ToOwnerID] = true
			}
			path = append(path, cycleVertex{
				clusterID: edge.ToClusterID,
				requestID: edge.ToRequestID,
				ownerID:   edge.ToOwnerID,
			})

			dfs(edge.ToRequestID, append(cosines, edge.Score))

			path = path[:len(path)-1]
			if edge.ToOwnerID != "" {
				delete(visitedOwners, edge.ToOwnerID)
			}
			delete(visitedClusters, edge.ToClusterID)
		}
	}

	dfs(startRequestID, nil)
	return drafts, nil
}

func (f *CycleFinder) acceptsQuality(scores []float64) bool {
	if len(scores) < minCycleLength {
		return false
	}

	minScore, maxScore := scores[0], scores[0]
	sum := 0.0
	for _, score := range scores {
		sum += score
		if score < minScore {
			minScore = score
		}
		if score > maxScore {
			maxScore = score
		}
	}

	average := sum / float64(len(scores))
	return average >= f.minAverage && maxScore-minScore <= f.maxScoreGap
}

func (f *CycleFinder) indexClosers(
	startRequestID int64,
	edges []entity.CandidateEdge,
) (map[int64]entity.CandidateEdge, int64, string) {
	closers := make(map[int64]entity.CandidateEdge, len(edges))
	var startClusterID int64
	var startOwnerID string

	for _, edge := range edges {
		if edge.ToRequestID != startRequestID ||
			edge.FromRequestID == edge.ToRequestID ||
			edge.FromClusterID == 0 ||
			edge.ToClusterID == 0 ||
			edge.FromClusterID == edge.ToClusterID ||
			edge.Score < f.threshold {
			continue
		}
		if startClusterID == 0 {
			startClusterID = edge.ToClusterID
			startOwnerID = edge.ToOwnerID
		}
		if edge.ToClusterID != startClusterID {
			continue
		}
		if previous, exists := closers[edge.FromRequestID]; !exists || edge.Score > previous.Score {
			closers[edge.FromRequestID] = edge
		}
	}
	return closers, startClusterID, startOwnerID
}

func (f *CycleFinder) loadLocalGraph(
	ctx context.Context,
	tx database.Tx,
	startRequestID int64,
) (map[int64][]entity.CandidateEdge, error) {
	adjacency := make(map[int64][]entity.CandidateEdge)
	frontier := []int64{startRequestID}
	expandedRequests := make(map[int64]bool)

	// Четырёх раскрытий frontier достаточно для путей из пяти кластеров.
	for level := 0; level < maxCycleLength-1 && len(frontier) > 0; level++ {
		sources := make([]int64, 0, len(frontier))
		sourceSet := make(map[int64]bool, len(frontier))
		for _, requestID := range frontier {
			if sourceSet[requestID] {
				continue
			}
			sources = append(sources, requestID)
			sourceSet[requestID] = true
		}
		if len(sources) == 0 {
			break
		}

		edges, err := f.loader.LoadOutgoingFrontier(ctx, tx, sources, f.outgoingK, f.threshold)
		if err != nil {
			return nil, err
		}

		bestEdges := make(map[[2]int64]entity.CandidateEdge, len(edges))
		for _, edge := range edges {
			if !sourceSet[edge.FromRequestID] ||
				edge.FromRequestID == edge.ToRequestID ||
				edge.FromClusterID == 0 ||
				edge.ToClusterID == 0 ||
				edge.FromClusterID == edge.ToClusterID ||
				edge.Score < f.threshold {
				continue
			}

			key := [2]int64{edge.FromRequestID, edge.ToRequestID}
			if previous, exists := bestEdges[key]; !exists || edge.Score > previous.Score {
				bestEdges[key] = edge
			}
		}

		nextRequests := make(map[int64]bool, len(bestEdges))
		for _, edge := range bestEdges {
			if expandedRequests[edge.FromRequestID] {
				continue
			}
			adjacency[edge.FromRequestID] = append(adjacency[edge.FromRequestID], edge)
			if edge.ToRequestID != startRequestID && !expandedRequests[edge.ToRequestID] {
				nextRequests[edge.ToRequestID] = true
			}
		}

		for fromRequestID := range adjacency {
			sort.SliceStable(adjacency[fromRequestID], func(i, j int) bool {
				if adjacency[fromRequestID][i].Score != adjacency[fromRequestID][j].Score {
					return adjacency[fromRequestID][i].Score > adjacency[fromRequestID][j].Score
				}
				return adjacency[fromRequestID][i].ToRequestID < adjacency[fromRequestID][j].ToRequestID
			})
		}

		nextFrontier := make([]int64, 0, len(nextRequests))
		for requestID := range nextRequests {
			nextFrontier = append(nextFrontier, requestID)
		}
		sort.Slice(nextFrontier, func(i, j int) bool { return nextFrontier[i] < nextFrontier[j] })
		for _, edge := range bestEdges {
			expandedRequests[edge.FromRequestID] = true
		}
		frontier = nextFrontier
	}

	return adjacency, nil
}

// dedupeDraft добавляет candidate в drafts, если такого набора кластеров-участников
// ещё нет. Возвращает ВСЕ уникальные цепочки (без сортировки и обрезки по score).
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
