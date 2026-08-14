package exchange_offer

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/api"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

func (h *Handler) Get(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	requestID, ok := api.PathInt64(c, "id")
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
			api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
		}
		return
	}

	api.SendOk(c, http.StatusOK, newExchangeOfferResponse(request))
}

func (h *Handler) List(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	requests, err := h.service.List(c.Request.Context(), userID)
	if err != nil {
		api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
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
