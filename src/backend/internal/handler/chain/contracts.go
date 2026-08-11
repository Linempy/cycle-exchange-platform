package chain

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	chainservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/chain"
)

// chainService описывает сценарии цепочек, используемые HTTP-обработчиком.
type chainService interface {
	List(ctx context.Context, userID string) ([]entity.Chain, error)
	ListForOffer(ctx context.Context, userID string, offerID int64) ([]entity.Chain, error)
	Get(ctx context.Context, userID string, chainID int64) (entity.Chain, error)
	Vote(ctx context.Context, userID string, chainID int64, input chainservice.VoteInput) (entity.ChainVote, error)
	WithdrawVote(ctx context.Context, userID string, chainID int64, input chainservice.VoteInput) error
	Confirm(ctx context.Context, userID string, chainID int64) (entity.ChainStatus, error)
	Think(ctx context.Context, userID string, chainID int64) error
	Decline(ctx context.Context, userID string, chainID int64) (bool, entity.ChainStatus, error)
	ListReplacements(ctx context.Context, userID string, chainID int64) ([]entity.ReplacementOption, error)
	SelectReplacement(ctx context.Context, userID string, chainID, replacementRequestID int64) error
	Handoff(ctx context.Context, chainID, requestID int64) (chainservice.FulfillmentResult, error)
	ConfirmReceipt(ctx context.Context, userID string, chainID, requestID int64) (chainservice.FulfillmentResult, error)
}
