package matching

import (
	"context"
	"sort"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

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
