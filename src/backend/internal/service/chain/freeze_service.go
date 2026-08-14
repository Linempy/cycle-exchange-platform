package chain

import (
	"context"
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

const FreezeTTL = 24 * time.Hour

// ChainRebuilder пересобирает цепочки после вычёркивания участников из
// конкурентов; реализует MatchingFacade.RepairAffectedChains.
type ChainRebuilder interface {
	RepairAffectedChains(ctx context.Context, tx database.Tx, affected []int64) error
	RebuildRequests(ctx context.Context, tx database.Tx, requestIDs []int64) error
}

type FreezeService struct {
	store     FreezeStore
	rebuilder ChainRebuilder
}

func NewFreezeService(store FreezeStore, rebuilder ChainRebuilder) *FreezeService {
	return &FreezeService{store: store, rebuilder: rebuilder}
}

func (s *FreezeService) Freeze(ctx context.Context, tx database.Tx, chainID int64) error {
	if s.store == nil {
		return entity.ErrChainRepositoryNotConfigured
	}
	requestIDs, err := s.store.LoadChainRequestIDs(ctx, tx, chainID)
	if err != nil {
		return err
	}
	if err := s.store.LockRequestsForFreeze(ctx, tx, requestIDs); err != nil {
		return err
	}
	if err := s.assertNoDoubleFreeze(ctx, tx, requestIDs); err != nil {
		return err
	}
	deadline := time.Now().Add(FreezeTTL)
	if err := s.store.FreezeChain(ctx, tx, chainID, deadline); err != nil {
		return err
	}
	if err := s.store.LockRequestsInChain(ctx, tx, chainID); err != nil {
		return err
	}
	if err := s.store.MarkItemsUnavailable(ctx, tx, chainID); err != nil {
		return err
	}
	released, err := s.store.ReleaseUnselectedFromChain(ctx, tx, chainID)
	if err != nil {
		return err
	}
	affected, err := s.store.ReleaseCompetitorsFromOtherChains(ctx, tx, chainID)
	if err != nil {
		return err
	}
	if s.rebuilder != nil {
		if err := s.rebuilder.RepairAffectedChains(ctx, tx, affected); err != nil {
			return err
		}
		return s.rebuilder.RebuildRequests(ctx, tx, released)
	}
	return nil
}

// ExpireDue applies every elapsed chain deadline in the caller transaction.
// It is intentionally request-driven: each public chain API call invokes it,
// which keeps externally visible state current without a background worker.
func (s *FreezeService) ExpireDue(ctx context.Context, tx database.Tx) error {
	if s.store == nil {
		return entity.ErrChainRepositoryNotConfigured
	}
	chainIDs, err := s.store.ListExpiredChainIDs(ctx, tx)
	if err != nil {
		return err
	}
	released := make([]int64, 0)
	for _, chainID := range chainIDs {
		expired, err := s.store.ExpireProposalIfDue(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if expired {
			continue
		}
		requestIDs, expired, err := s.store.ExpireFrozenIfDue(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if expired {
			released = append(released, requestIDs...)
		}
	}
	if len(released) > 0 && s.rebuilder != nil {
		return s.rebuilder.RebuildRequests(ctx, tx, released)
	}
	return nil
}

func (s *FreezeService) assertNoDoubleFreeze(ctx context.Context, tx database.Tx, requestIDs []int64) error {
	for _, requestID := range requestIDs {
		status, err := s.store.LoadRequestLiveChainStatus(ctx, tx, requestID)
		if err != nil {
			return err
		}
		if status == entity.ChainStatusFrozen {
			return entity.ErrRequestInTwoFrozenChains
		}
	}
	return nil
}
