package exchange_offer

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/api"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/middleware"
	offerservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/exchange_offer"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/validator"
)

// Handler обрабатывает HTTP-запросы CRUD заявок на обмен.
// Идентификатор пользователя берётся только из JWT-мидлвари, а не из тела запроса.
type Handler struct {
	service exchangeOfferService
}

func NewHandler(service exchangeOfferService) *Handler {
	return &Handler{service: service}
}

type createBody struct {
	OfferedItemID     int64  `json:"offeredItemId" validate:"required,gt=0"`
	WantedDescription string `json:"wantedDescription" validate:"not_empty,max=5000"`
	WantedCategory    string `json:"wantedCategory" validate:"not_empty,max=100"`
}

type updateBody struct {
	OfferedItemID     int64  `json:"offeredItemId" validate:"required,gt=0"`
	WantedDescription string `json:"wantedDescription" validate:"not_empty,max=5000"`
	WantedCategory    string `json:"wantedCategory" validate:"not_empty,max=100"`
	Version           int64  `json:"version" validate:"required,gt=0"`
}

type deleteQuery struct {
	Version int64 `schema:"version" validate:"required,gt=0"`
}

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

func (h *Handler) Create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var body createBody
	if err := validator.BindJSON(&body, c.Request); err != nil {
		var jsonSyntaxErr *json.SyntaxError
		if errors.As(err, &jsonSyntaxErr) {
			api.SendError(c, http.StatusBadRequest, "invalid JSON")
			return
		}
		api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	created, err := h.service.Create(c.Request.Context(), userID, offerservice.CreateInput{
		OfferedItemID:     body.OfferedItemID,
		WantedDescription: body.WantedDescription,
		WantedCategory:    body.WantedCategory,
	})
	if err != nil {
		var ve validator.Error
		switch {
		case errors.As(err, &ve):
			api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, entity.ErrOfferedItemUnavailable):
			api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		default:
			api.SendError(c, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	api.SendOk(c, http.StatusCreated, newExchangeOfferResponse(created))
}

func (h *Handler) Get(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	requestID, ok := pathID(c)
	if !ok {
		return
	}

	request, err := h.service.Get(c.Request.Context(), userID, requestID)
	if err != nil {
		switch {
		case errors.Is(err, entity.ErrExchangeOfferNotFound):
			api.SendError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, entity.ErrExchangeOfferForbidden):
			api.SendError(c, http.StatusForbidden, err.Error())
		default:
			api.SendError(c, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	api.SendOk(c, http.StatusOK, newExchangeOfferResponse(request))
}

func (h *Handler) List(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	requests, err := h.service.List(c.Request.Context(), userID)
	if err != nil {
		api.SendError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	response := make([]exchangeOfferListResponse, 0, len(requests))
	for _, request := range requests {
		response = append(response, exchangeOfferListResponse{
			exchangeOfferResponse: newExchangeOfferResponse(request.ExchangeOffer),
			OfferedItemTitle:      request.OfferedItemTitle,
		})
	}

	api.SendOk(c, http.StatusOK, response)
}

func (h *Handler) Update(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	requestID, ok := pathID(c)
	if !ok {
		return
	}

	var body updateBody
	if err := validator.BindJSON(&body, c.Request); err != nil {
		var jsonSyntaxErr *json.SyntaxError
		if errors.As(err, &jsonSyntaxErr) {
			api.SendError(c, http.StatusBadRequest, "invalid JSON")
			return
		}
		api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	updated, err := h.service.Update(c.Request.Context(), userID, requestID, offerservice.UpdateInput{
		OfferedItemID:     body.OfferedItemID,
		WantedDescription: body.WantedDescription,
		WantedCategory:    body.WantedCategory,
		Version:           body.Version,
	})
	if err != nil {
		var ve validator.Error
		switch {
		case errors.As(err, &ve):
			api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, entity.ErrExchangeOfferNotFound):
			api.SendError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, entity.ErrExchangeOfferForbidden):
			api.SendError(c, http.StatusForbidden, err.Error())
		case errors.Is(err, entity.ErrExchangeOfferVersionConflict),
			errors.Is(err, entity.ErrExchangeOfferLocked):
			api.SendError(c, http.StatusConflict, err.Error())
		case errors.Is(err, entity.ErrOfferedItemUnavailable):
			api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		default:
			api.SendError(c, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	api.SendOk(c, http.StatusOK, newExchangeOfferResponse(updated))
}

func (h *Handler) Delete(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	requestID, ok := pathID(c)
	if !ok {
		return
	}

	var query deleteQuery
	if err := validator.BindQuery(&query, c.Request); err != nil {
		api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if err := h.service.Delete(c.Request.Context(), userID, requestID, query.Version); err != nil {
		var ve validator.Error
		switch {
		case errors.As(err, &ve):
			api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, entity.ErrExchangeOfferNotFound):
			api.SendError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, entity.ErrExchangeOfferForbidden):
			api.SendError(c, http.StatusForbidden, err.Error())
		case errors.Is(err, entity.ErrExchangeOfferVersionConflict),
			errors.Is(err, entity.ErrExchangeOfferLocked):
			api.SendError(c, http.StatusConflict, err.Error())
		default:
			api.SendError(c, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	c.Status(http.StatusNoContent)
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

func currentUserID(c *gin.Context) (string, bool) {
	userID, ok := middleware.UserID(c)
	if !ok {
		api.SendError(c, http.StatusUnauthorized, "authentication is required")
		return "", false
	}
	return userID.String(), true
}

func pathID(c *gin.Context) (int64, bool) {
	requestID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || requestID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
		return 0, false
	}
	return requestID, true
}
