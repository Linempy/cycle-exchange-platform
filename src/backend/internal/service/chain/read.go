package chain

import (
	"context"
	"errors"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

func (s *Service) List(ctx context.Context, userID string) ([]entity.Chain, error) {
	if s.repository == nil {
		return nil, entity.ErrChainRepositoryNotConfigured
	}
	if err := s.expireDue(ctx); err != nil {
		return nil, err
	}
	return s.repository.List(ctx, userID)
}

// ListForOffer возвращает актуальные цепочки конкретной заявки её владельцу.

func (s *Service) ListForOffer(ctx context.Context, userID string, offerID int64) ([]entity.Chain, error) {
	if s.repository == nil {
		return nil, entity.ErrChainRepositoryNotConfigured
	}
	if offerID <= 0 {
		return nil, entity.ErrExchangeOfferNotFound
	}
	if err := s.expireDue(ctx); err != nil {
		return nil, err
	}
	return s.repository.ListForOffer(ctx, userID, offerID)
}

// Get возвращает цепочку только тогда, когда пользователь является её участником.

func (s *Service) Get(ctx context.Context, userID string, chainID int64) (entity.Chain, error) {
	if s.repository == nil {
		return entity.Chain{}, entity.ErrChainRepositoryNotConfigured
	}
	if err := s.expireDue(ctx); err != nil {
		return entity.Chain{}, err
	}
	chain, err := s.repository.Get(ctx, userID, chainID)
	if !errors.Is(err, entity.ErrChainNotFound) {
		return chain, err
	}
	expired, eventErr := s.repository.HasDeadlineEvent(ctx, userID, chainID)
	if eventErr != nil {
		return entity.Chain{}, eventErr
	}
	if expired {
		return entity.Chain{}, entity.ErrChainConfirmationExpired
	}
	return entity.Chain{}, err
}

func (s *Service) expireDue(ctx context.Context) error {
	if s.freezer == nil {
		return nil
	}
	if s.transactions == nil {
		return entity.ErrChainRepositoryNotConfigured
	}
	return s.transactions.WithinTransaction(ctx, func(tx database.Tx) error {
		return s.freezer.ExpireDue(ctx, tx)
	})
}

// Vote records an idempotent response and atomically proposes a chain when the
// approved responses form a closed cycle through every chain position.
