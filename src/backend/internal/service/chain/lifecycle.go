package chain

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

// ListChainsContainingRequest возвращает цепочки, где участвует заявка.
func (s *Service) ListChainsContainingRequest(ctx context.Context, tx database.Tx, requestID int64) ([]int64, error) {
	if s.repository == nil {
		return nil, entity.ErrChainRepositoryNotConfigured
	}
	return s.repository.ListChainsContainingRequest(ctx, tx, requestID)
}

// DeleteRequestParticipation удаляет участие заявки в цепочках и её голоса.
func (s *Service) DeleteRequestParticipation(ctx context.Context, tx database.Tx, requestID int64) error {
	if s.repository == nil {
		return entity.ErrChainRepositoryNotConfigured
	}
	return s.repository.DeleteRequestParticipation(ctx, tx, requestID)
}

// DeleteChain удаляет цепочку целиком каскадом.
func (s *Service) DeleteChain(ctx context.Context, tx database.Tx, chainID int64) error {
	if s.repository == nil {
		return entity.ErrChainRepositoryNotConfigured
	}
	return s.repository.DeleteChain(ctx, tx, chainID)
}

// LoadChainRequestIDs возвращает заявки участников цепочки.
func (s *Service) LoadChainRequestIDs(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error) {
	if s.repository == nil {
		return nil, entity.ErrChainRepositoryNotConfigured
	}
	return s.repository.LoadChainRequestIDs(ctx, tx, chainID)
}

func (s *Service) LoadActiveChainRequestIDs(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error) {
	if s.repository == nil {
		return nil, entity.ErrChainRepositoryNotConfigured
	}
	return s.repository.LoadActiveChainRequestIDs(ctx, tx, chainID)
}
