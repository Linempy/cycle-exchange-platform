package exchange_offer

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

// ExchangeOfferRepository описывает хранилище предложений обмена, необходимое сервису.
// Реализация должна атомарно инвалидировать кандидатные цепочки при изменении заявки.
type ExchangeOfferRepository interface {
	Create(ctx context.Context, tx database.Tx, request entity.ExchangeOffer) (entity.ExchangeOffer, error)
	Get(ctx context.Context, userID string, requestID int64) (entity.ExchangeOffer, error)
	List(ctx context.Context, userID string) ([]entity.ExchangeOfferListItem, error)
	Update(ctx context.Context, tx database.Tx, request entity.ExchangeOffer, expectedVersion int64) (entity.ExchangeOffer, error)
	Archive(ctx context.Context, tx database.Tx, userID string, requestID, expectedVersion int64) (entity.ExchangeOffer, error)
}

// MatchingFacade описывает нужную сервису заявок часть подсистемы matching.
// Интерфейс объявлен у потребителя, чтобы реализация matching не навязывала
// свой контракт остальному приложению.
type MatchingFacade interface {
	RebuildForRequest(ctx context.Context, tx database.Tx, requestID int64) ([]entity.ChainDraft, error)
	RemoveRequest(ctx context.Context, tx database.Tx, requestID int64) error
}
