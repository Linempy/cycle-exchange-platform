package exchange_offer

import (
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

type exchangeOfferResponse struct {
	ID                int64                `json:"id"`
	OfferedItemID     int64                `json:"offeredItemId"`
	WantedDescription string               `json:"wantedDescription"`
	WantedCategory    string               `json:"wantedCategory"`
	Status            entity.RequestStatus `json:"status"`
	Version           int64                `json:"version"`
	CreatedAt         string               `json:"createdAt"`
	UpdatedAt         string               `json:"updatedAt"`
}

type exchangeOfferListResponse struct {
	exchangeOfferResponse
	OfferedItemTitle string `json:"offeredItemTitle"`
}

func newExchangeOfferResponse(offer entity.ExchangeOffer) exchangeOfferResponse {
	return exchangeOfferResponse{
		ID:                offer.ID,
		OfferedItemID:     offer.OfferedItemID,
		WantedDescription: offer.WantedDescription,
		WantedCategory:    offer.WantedCategory,
		Status:            offer.Status,
		Version:           offer.Version,
		CreatedAt:         offer.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         offer.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
