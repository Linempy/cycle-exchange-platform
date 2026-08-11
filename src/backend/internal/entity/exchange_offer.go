package entity

import "time"

// ExchangeOffer — предложение пользователя: какой товар он отдаёт и что хочет получить.
type ExchangeOffer struct {
	ID                int64
	UserID            string
	OfferedItemID     int64
	WantedDescription string
	// WantedCategory — текстовое имя категории желаемого товара (например, "Телефоны").
	WantedCategory string
	WantEmbedding  []float32
	Status         RequestStatus
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ExchangeOfferListItem содержит предложение и название отдаваемого товара,
// полученные одним запросом без N+1.
type ExchangeOfferListItem struct {
	ExchangeOffer
	OfferedItemTitle string
}
