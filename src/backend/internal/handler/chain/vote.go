package chain

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/api"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	chainservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/chain"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/validator"
)

func (h *Handler) Vote(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	chainID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}

	var body voteRequest
	if !api.BindJSON(c, &body) {
		return
	}

	vote, err := h.service.Vote(c.Request.Context(), userID, chainID, chainservice.VoteInput{
		RequestID:       body.RequestID,
		TargetRequestID: body.TargetRequestID,
	})
	if err != nil {
		var ve validator.Error
		switch {
		case errors.As(err, &ve):
			api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, entity.ErrChainNotFound):
			api.SendError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, entity.ErrChainVoteForbidden):
			api.SendError(c, http.StatusForbidden, err.Error())
		case errors.Is(err, entity.ErrChainNotCandidate):
			api.SendError(c, http.StatusConflict, err.Error())
		case errors.Is(err, entity.ErrInvalidVoteTarget):
			api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		default:
			api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
		}
		return
	}

	api.SendOk(c, http.StatusOK, voteResponse{
		ChainID:         vote.ChainID,
		RequestID:       vote.RequestID,
		TargetRequestID: vote.TargetRequestID,
		Vote:            vote.Vote,
		VotedAt:         vote.VotedAt.UTC().Format(time.RFC3339),
		ChainStatus:     vote.ChainStatus,
	})
}

func (h *Handler) WithdrawVote(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	chainID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}
	var query withdrawVoteQuery
	if err := validator.BindQuery(&query, c.Request); err != nil {
		api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	err := h.service.WithdrawVote(c.Request.Context(), userID, chainID, chainservice.VoteInput{
		RequestID:       query.RequestID,
		TargetRequestID: query.TargetRequestID,
	})
	if err != nil {
		var ve validator.Error
		switch {
		case errors.As(err, &ve):
			api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, entity.ErrChainNotFound):
			api.SendError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, entity.ErrChainVoteForbidden):
			api.SendError(c, http.StatusForbidden, err.Error())
		case errors.Is(err, entity.ErrChainNotCandidate):
			api.SendError(c, http.StatusConflict, err.Error())
		case errors.Is(err, entity.ErrInvalidVoteTarget):
			api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		default:
			api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
		}
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) Confirm(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}

	chainID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}

	status, err := h.service.Confirm(c.Request.Context(), userID, chainID)
	if err != nil {
		switch {
		case errors.Is(err, entity.ErrChainNotFound):
			api.SendError(c, http.StatusNotFound, err.Error())
		case errors.Is(err, entity.ErrChainNotProposed):
			api.SendError(c, http.StatusConflict, err.Error())
		case errors.Is(err, entity.ErrRequestInTwoFrozenChains):
			api.SendError(c, http.StatusConflict, err.Error())
		case errors.Is(err, entity.ErrChainVoteForbidden):
			api.SendError(c, http.StatusForbidden, err.Error())
		case errors.Is(err, entity.ErrChainConfirmationExpired):
			api.SendError(c, http.StatusGone, err.Error())
		default:
			log.Printf("confirm chain failed: %v", err)
			api.SendError(c, http.StatusInternalServerError, "внутренняя ошибка сервера")
		}
		return
	}

	api.SendOk(c, http.StatusOK, gin.H{"chainId": chainID, "status": status})
}

func (h *Handler) Think(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}
	chainID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}
	if err := h.service.Think(c.Request.Context(), userID, chainID); err != nil {
		DetermineError(c, err)
		return
	}
	api.SendOk(c, http.StatusOK, gin.H{"chainId": chainID, "vote": entity.VoteThinking})
}

// Unconfirm returns the caller's round-two decision to pending. It does not
// remove the candidate response, so the participant can confirm again later.
func (h *Handler) Unconfirm(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}
	chainID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}
	status, err := h.service.Unconfirm(c.Request.Context(), userID, chainID)
	if err != nil {
		DetermineError(c, err)
		return
	}
	api.SendOk(c, http.StatusOK, gin.H{"chainId": chainID, "status": status, "vote": entity.VotePending})
}

// Decline — отказ; в ответе есть флаг доступности быстрой замены.
func (h *Handler) Decline(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}
	chainID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}
	replacementAvailable, status, err := h.service.Decline(c.Request.Context(), userID, chainID)
	if err != nil {
		DetermineError(c, err)
		return
	}
	api.SendOk(c, http.StatusOK, gin.H{
		"chainId": chainID, "status": status, "replacementAvailable": replacementAvailable,
	})
}

func (h *Handler) SelectReplacement(c *gin.Context) {
	userID, ok := api.CurrentUserString(c)
	if !ok {
		return
	}
	chainID, ok := api.PathInt64(c, "id")
	if !ok {
		return
	}
	var body selectReplacementRequest
	if !api.BindJSON(c, &body) {
		return
	}
	if err := h.service.SelectReplacement(c.Request.Context(), userID, chainID, body.RequestID); err != nil {
		if errors.Is(err, entity.ErrInvalidVoteTarget) {
			api.SendError(c, http.StatusUnprocessableEntity, err.Error())
			return
		}
		DetermineError(c, err)
		return
	}
	api.SendOk(c, http.StatusOK, gin.H{"chainId": chainID, "requestId": body.RequestID, "status": entity.ChainStatusProposed})
}
