package matching

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/utils/ranker"
)

// ClusterSynchronizer описывает часть matching, отвечающую за актуальное
// членство заявки в кластере.
type ClusterSynchronizer interface {
	Synchronize(ctx context.Context, tx database.Tx, offerID int64) error
	Remove(ctx context.Context, tx database.Tx, offerID int64) error
}

// CycleSearcher описывает поиск вариантов цепочки после актуализации кластера.
type CycleSearcher interface {
	Find(ctx context.Context, tx database.Tx, startRequestID int64) ([]entity.ChainDraft, error)
}

// ChainLifecycle описывает операции над цепочками, которые выполняет matcher
// при пересборке после выбытия заявки/участника.
type ChainLifecycle interface {
	SaveCandidates(ctx context.Context, tx database.Tx, drafts []entity.ChainDraft) error
	LoadChainRequestIDs(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error)
	ListChainsContainingRequest(ctx context.Context, tx database.Tx, requestID int64) ([]int64, error)
	DeleteRequestParticipation(ctx context.Context, tx database.Tx, requestID int64) error
	DeleteChain(ctx context.Context, tx database.Tx, chainID int64) error
}

// MatchingFacade связывает CRUD заявок с кластеризацией и поиском вариантов цепочек.
type MatchingFacade struct {
	clusters ClusterSynchronizer
	cycles   CycleSearcher
	chains   ChainLifecycle
	ranker   *ranker.ChainScoreCalculator
}

func (f *MatchingFacade) WithRanker(r *ranker.ChainScoreCalculator) *MatchingFacade {
	f.ranker = r
	return f
}

// NewFacade создаёт рабочий matching-фасад.
func NewFacade(clusters ClusterSynchronizer, cycles CycleSearcher, chains ChainLifecycle) *MatchingFacade {
	return &MatchingFacade{clusters: clusters, cycles: cycles, chains: chains}
}

// RebuildForRequest синхронно актуализирует производные данные заявки.
func (f *MatchingFacade) RebuildForRequest(
	ctx context.Context,
	tx database.Tx,
	requestID int64,
) ([]entity.ChainDraft, error) {
	if f.clusters == nil {
		return nil, entity.ErrClusterNotConfigured
	}
	// chain_participants keeps a foreign key to clusters.  Therefore all
	// candidate chains built from the old cluster must be removed before
	// Synchronize removes the request's old membership (and possibly the empty
	// cluster itself).
	affectedRequestIDs, err := f.detachAffectedCandidates(ctx, tx, requestID)
	if err != nil {
		return nil, err
	}
	if err := f.clusters.Synchronize(ctx, tx, requestID); err != nil {
		return nil, err
	}
	drafts, err := f.findAndSaveCandidates(ctx, tx, requestID)
	if err != nil {
		return nil, err
	}
	if err := f.rebuildCandidateRequests(ctx, tx, affectedRequestIDs, requestID); err != nil {
		return nil, err
	}
	return drafts, nil
}

// detachAffectedCandidates removes all candidate-chain rows that reference
// the request's current cluster and returns their selected requests for a
// subsequent search.  The deletion must happen before the cluster membership
// is changed, otherwise PostgreSQL correctly prevents deleting an empty
// cluster referenced by chain_participants.
func (f *MatchingFacade) detachAffectedCandidates(
	ctx context.Context,
	tx database.Tx,
	requestID int64,
) ([]int64, error) {
	if f.chains == nil {
		return nil, nil
	}
	affected, err := f.chains.ListChainsContainingRequest(ctx, tx, requestID)
	if err != nil {
		return nil, err
	}
	requestIDs := make([]int64, 0)
	for _, chainID := range affected {
		ids, err := f.chains.LoadChainRequestIDs(ctx, tx, chainID)
		if err != nil {
			return nil, err
		}
		if err := f.chains.DeleteChain(ctx, tx, chainID); err != nil {
			return nil, err
		}
		requestIDs = append(requestIDs, ids...)
	}
	return requestIDs, nil
}

func (f *MatchingFacade) findAndSaveCandidates(
	ctx context.Context,
	tx database.Tx,
	requestID int64,
) ([]entity.ChainDraft, error) {
	if f.cycles == nil {
		return []entity.ChainDraft{}, nil
	}
	drafts, err := f.cycles.Find(ctx, tx, requestID)
	if err != nil {
		return nil, err
	}
	if len(drafts) > 0 {
		if f.chains == nil {
			return nil, entity.ErrChainRepositoryNotConfigured
		}
		if f.ranker == nil {
			return nil, entity.ErrScoreNotConfigured
		}
		for i := range drafts {
			score, err := f.ranker.Score(chainStateFromDraft(drafts[i]))
			if err != nil {
				return nil, err
			}
			drafts[i].Score = score
		}
		if err := f.chains.SaveCandidates(ctx, tx, drafts); err != nil {
			return nil, err
		}
	}
	return drafts, nil
}

func (f *MatchingFacade) rebuildCandidateRequests(
	ctx context.Context,
	tx database.Tx,
	requestIDs []int64,
	exceptID int64,
) error {
	seen := make(map[int64]struct{}, len(requestIDs))
	for _, requestID := range requestIDs {
		if requestID <= 0 || requestID == exceptID {
			continue
		}
		if _, exists := seen[requestID]; exists {
			continue
		}
		seen[requestID] = struct{}{}
		if _, err := f.findAndSaveCandidates(ctx, tx, requestID); err != nil {
			return err
		}
	}
	return nil
}

// RemoveRequest исключает заявку из производных данных: вычищает её участие
// в цепочках, убирает из кластера и пересобирает/удаляет затронутые цепочки.
func (f *MatchingFacade) RemoveRequest(ctx context.Context, tx database.Tx, requestID int64) error {
	if f.clusters == nil {
		return entity.ErrClusterNotConfigured
	}
	var affected []int64
	if f.chains != nil {
		var err error
		affected, err = f.chains.ListChainsContainingRequest(ctx, tx, requestID)
		if err != nil {
			return err
		}
		if err := f.chains.DeleteRequestParticipation(ctx, tx, requestID); err != nil {
			return err
		}
	}
	if err := f.clusters.Remove(ctx, tx, requestID); err != nil {
		return err
	}
	return f.RepairAffectedChains(ctx, tx, affected)
}

// RepairAffectedChains приводит затронутые цепочки в актуальное состояние:
// удаляет прежние варианты и запускает новый круг поиска от первого
// оставшегося участника каждой, либо удаляет опустевшие.
func (f *MatchingFacade) RepairAffectedChains(ctx context.Context, tx database.Tx, affected []int64) error {
	for _, chainID := range affected {
		requestIDs, err := f.chains.LoadChainRequestIDs(ctx, tx, chainID)
		if err != nil {
			return err
		}
		// Прежний вариант цепочки больше не актуален (статус/состав изменились).
		if err := f.chains.DeleteChain(ctx, tx, chainID); err != nil {
			return err
		}
		if len(requestIDs) == 0 {
			continue
		}
		// Новый круг поиска от первого оставшегося участника.
		if _, err := f.RebuildForRequest(ctx, tx, requestIDs[0]); err != nil {
			return err
		}
	}
	return nil
}

// RebuildRequests запускает новый поиск для освобождённых заявок, исключая повторы.
func (f *MatchingFacade) RebuildRequests(ctx context.Context, tx database.Tx, requestIDs []int64) error {
	seen := make(map[int64]struct{}, len(requestIDs))
	for _, requestID := range requestIDs {
		if requestID <= 0 {
			continue
		}
		if _, exists := seen[requestID]; exists {
			continue
		}
		seen[requestID] = struct{}{}
		if _, err := f.RebuildForRequest(ctx, tx, requestID); err != nil {
			return err
		}
	}
	return nil
}

func chainStateFromDraft(draft entity.ChainDraft) ranker.ChainState {
	return ranker.ChainState{
		Count:                   len(draft.Participants),
		Stage:                   ranker.ChainStateCandidate,
		Event:                   ranker.EventAdd,
		EdgeCosines:             draft.EdgeCosines,
		ParticipantReliability:  draft.ParticipantReliability,
		ParticipantClusterSizes: draft.ClusterSizes,
		ApprovedVotes:           0,
	}
}
