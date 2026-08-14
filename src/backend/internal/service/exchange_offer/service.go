package exchange_offer

import (
	"context"
	"fmt"
	"strings"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/infrastructure/embedding"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/validator"
)

type CreateInput struct {
	OfferedItemID     int64  `json:"offeredItemId" validate:"required,gt=0"`
	WantedDescription string `json:"wantedDescription" validate:"not_empty,max=5000"`
	WantedCategory    string `json:"wantedCategory" validate:"not_empty,max=100"`
}

type UpdateInput struct {
	OfferedItemID     int64  `json:"offeredItemId" validate:"required,gt=0"`
	WantedDescription string `json:"wantedDescription" validate:"not_empty,max=5000"`
	WantedCategory    string `json:"wantedCategory" validate:"not_empty,max=100"`
	Version           int64  `json:"version" validate:"required,gt=0"`
}

type deleteInput struct {
	Version int64 `json:"version" validate:"required,gt=0"`
}

type Service struct {
	repository   ExchangeOfferRepository
	embedding    embedding.Client
	matching     MatchingFacade
	transactions database.TransactionManager
}

func NewService(
	repository ExchangeOfferRepository,
	embeddingClient embedding.Client,
	matchingFacade MatchingFacade,
	transactionManager database.TransactionManager,
) *Service {
	return &Service{
		repository:   repository,
		embedding:    embeddingClient,
		matching:     matchingFacade,
		transactions: transactionManager,
	}
}

func (s *Service) Create(ctx context.Context, userID string, input CreateInput) (entity.ExchangeOffer, error) {
	if err := validator.Validate(&input); err != nil {
		return entity.ExchangeOffer{}, err
	}

	embeddingValue, err := s.embedWanted(ctx, input.WantedDescription)
	if err != nil {
		return entity.ExchangeOffer{}, err
	}

	if s.transactions == nil || s.matching == nil {
		return entity.ExchangeOffer{}, entity.ErrMatchingNotConfigured
	}

	var created entity.ExchangeOffer
	err = s.transactions.WithinTransaction(ctx, func(tx database.Tx) error {
		var createErr error
		created, createErr = s.repository.Create(ctx, tx, entity.ExchangeOffer{
			UserID:            userID,
			OfferedItemID:     input.OfferedItemID,
			WantedDescription: strings.TrimSpace(input.WantedDescription),
			WantedCategory:    strings.TrimSpace(input.WantedCategory),
			WantEmbedding:     embeddingValue,
			Status:            entity.RequestStatusActive,
			Version:           1,
		})
		if createErr != nil {
			return createErr
		}
		_, matchingErr := s.matching.RebuildForRequest(ctx, tx, created.ID)
		return matchingErr
	})
	if err != nil {
		return entity.ExchangeOffer{}, err
	}

	return created, nil
}

func (s *Service) Get(ctx context.Context, userID string, requestID int64) (entity.ExchangeOffer, error) {
	return s.repository.Get(ctx, userID, requestID)
}

func (s *Service) List(ctx context.Context, userID string) ([]entity.ExchangeOfferListItem, error) {
	return s.repository.List(ctx, userID)
}

func (s *Service) Update(ctx context.Context, userID string, requestID int64, input UpdateInput) (entity.ExchangeOffer, error) {
	if err := validator.Validate(&input); err != nil {
		return entity.ExchangeOffer{}, err
	}

	embeddingValue, err := s.embedWanted(ctx, input.WantedDescription)
	if err != nil {
		return entity.ExchangeOffer{}, err
	}

	if s.transactions == nil || s.matching == nil {
		return entity.ExchangeOffer{}, entity.ErrMatchingNotConfigured
	}

	var updated entity.ExchangeOffer
	err = s.transactions.WithinTransaction(ctx, func(tx database.Tx) error {
		var updateErr error
		updated, updateErr = s.repository.Update(ctx, tx, entity.ExchangeOffer{
			ID:                requestID,
			UserID:            userID,
			OfferedItemID:     input.OfferedItemID,
			WantedDescription: strings.TrimSpace(input.WantedDescription),
			WantedCategory:    strings.TrimSpace(input.WantedCategory),
			WantEmbedding:     embeddingValue,
		}, input.Version)
		if updateErr != nil {
			return updateErr
		}
		_, matchingErr := s.matching.RebuildForRequest(ctx, tx, updated.ID)
		return matchingErr
	})
	if err != nil {
		return entity.ExchangeOffer{}, err
	}

	return updated, nil
}

func (s *Service) Delete(ctx context.Context, userID string, requestID, version int64) error {
	if err := validator.Validate(&deleteInput{Version: version}); err != nil {
		return err
	}

	if s.transactions == nil || s.matching == nil {
		return entity.ErrMatchingNotConfigured
	}

	var archived entity.ExchangeOffer
	err := s.transactions.WithinTransaction(ctx, func(tx database.Tx) error {
		var archiveErr error
		archived, archiveErr = s.repository.Archive(ctx, tx, userID, requestID, version)
		if archiveErr != nil {
			return archiveErr
		}
		return s.matching.RemoveRequest(ctx, tx, archived.ID)
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *Service) embedWanted(ctx context.Context, description string) ([]float32, error) {
	if s.embedding == nil {
		return nil, entity.ErrEmbeddingNotConfigured
	}

	prompt := "query: " + strings.TrimSpace(description)

	result, err := s.embedding.Embed(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("embed wanted description: %w", err)
	}
	if len(result) == 0 {
		return nil, entity.ErrEmptyEmbedding
	}

	return result, nil
}
