package chain

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/api"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

func DetermineError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, entity.ErrChainNotFound):
		api.SendError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, entity.ErrChainNotProposed):
		api.SendError(c, http.StatusConflict, err.Error())
	case errors.Is(err, entity.ErrChainConfirmationExpired):
		api.SendError(c, http.StatusGone, err.Error())
	case errors.Is(err, entity.ErrChainConfirmationNotFound):
		api.SendError(c, http.StatusConflict, err.Error())
	case errors.Is(err, entity.ErrChainNotReadyForHandoff), errors.Is(err, entity.ErrChainHandoffPending):
		api.SendError(c, http.StatusConflict, err.Error())
	case errors.Is(err, entity.ErrChainVoteForbidden):
		api.SendError(c, http.StatusForbidden, err.Error())
	case errors.Is(err, entity.ErrChainReceiptForbidden):
		api.SendError(c, http.StatusForbidden, err.Error())
	case errors.Is(err, entity.ErrHandoffRequestInvalid):
		api.SendError(c, http.StatusUnprocessableEntity, err.Error())
	default:
		log.Printf("chain request failed: %v", err)
		api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
	}
}
