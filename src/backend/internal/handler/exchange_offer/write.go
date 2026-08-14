package exchange_offer

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/api"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	offerservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/exchange_offer"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/validator"
)

func (h *Handler) Create(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	var body createBody
	if !api.BindJSON(c, &body) {
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
			api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
		}
		return
	}

	api.SendOk(c, http.StatusCreated, newExchangeOfferResponse(created))
}

func (h *Handler) Update(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	requestID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}

	var body updateBody
	if !api.BindJSON(c, &body) {
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
			api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
		}
		return
	}

	api.SendOk(c, http.StatusOK, newExchangeOfferResponse(updated))
}

func (h *Handler) Delete(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	requestID, ok := api.PathInt64(c, "id")
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
			api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
		}
		return
	}

	c.Status(http.StatusNoContent)
}
