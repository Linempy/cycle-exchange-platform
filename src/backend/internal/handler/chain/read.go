package chain

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/api"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

func (h *Handler) List(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	chains, err := h.service.List(c.Request.Context(), userID)
	if err != nil {
		api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
		return
	}

	response := make([]chainResponse, 0, len(chains))
	for _, chain := range chains {
		response = append(response, newChainResponse(chain, userID))
	}
	api.SendOk(c, http.StatusOK, response)
}

// ExchangeOptions — готовые варианты получения по заявке владельца.
func (h *Handler) ExchangeOptions(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	offerID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}

	chains, err := h.service.ListForOffer(c.Request.Context(), userID, offerID)
	if err != nil {
		if errors.Is(err, entity.ErrExchangeOfferNotFound) {
			api.SendError(c, http.StatusNotFound, err.Error())
			return
		}
		api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
		return
	}

	response := make([]exchangeOptionsResponse, 0, len(chains))
	for _, chain := range chains {
		response = append(response, newExchangeOptionsResponse(chain))
	}
	api.SendOk(c, http.StatusOK, response)
}

func (h *Handler) Get(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	chainID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}

	chain, err := h.service.Get(c.Request.Context(), userID, chainID)
	if err != nil {
		if errors.Is(err, entity.ErrChainNotFound) {
			api.SendError(c, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, entity.ErrChainConfirmationExpired) {
			api.SendError(c, http.StatusGone, err.Error())
			return
		}
		api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
		return
	}
	api.SendOk(c, http.StatusOK, newChainResponse(chain, userID))
}

func (h *Handler) ListReplacements(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}
	chainID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}
	options, err := h.service.ListReplacements(c.Request.Context(), userID, chainID)
	if err != nil {
		DetermineError(c, err)
		return
	}
	response := make([]replacementResponse, 0, len(options))
	for _, option := range options {
		response = append(response, replacementResponse{
			RequestID: option.RequestID, OfferedItemID: option.OfferedItemID,
			Title: option.Title, Description: option.Description,
			WantedDescription: option.WantedDescription, ImageURL: option.ImageURL,
			Reliability: option.Reliability, RespondedAt: option.RespondedAt.UTC().Format(time.RFC3339),
		})
	}
	api.SendOk(c, http.StatusOK, response)
}
