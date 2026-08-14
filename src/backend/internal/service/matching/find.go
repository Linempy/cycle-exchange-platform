package matching

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

// Find — все уникальные простые циклы 2–5 от startRequestID; score/top-N — у Ranker на фасаде.
// Пустой результат штатен.
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
