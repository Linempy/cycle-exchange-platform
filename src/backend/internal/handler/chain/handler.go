package chain

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/api"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/middleware"
	chainservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/chain"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/validator"
)

// Handler обрабатывает чтение доступных пользователю цепочек.
type Handler struct {
	service chainService
}

// NewHandler создаёт HTTP-обработчик цепочек.
func NewHandler(service chainService) *Handler {
	return &Handler{service: service}
}

type chainResponse struct {
	ID                   int64                      `json:"id"`
	Status               entity.ChainStatus         `json:"status"`
	Score                float64                    `json:"score"`
	Length               int                        `json:"length"`
	Version              int64                      `json:"version"`
	CurrentRequestID     int64                      `json:"currentRequestId"`
	CurrentPosition      int                        `json:"currentPosition"`
	GivesToPosition      int                        `json:"givesToPosition"`
	ReceivesFromPosition int                        `json:"receivesFromPosition"`
	FreezeDeadlineAt     *string                    `json:"freezeDeadlineAt,omitempty"`
	InvalidReason        *string                    `json:"invalidReason,omitempty"`
	CreatedAt            string                     `json:"createdAt"`
	UpdatedAt            string                     `json:"updatedAt"`
	Participants         []chainParticipantResponse `json:"participants"`
}

type chainParticipantResponse struct {
	ClusterID              int64                `json:"clusterId"`
	RequestID              int64                `json:"requestId"`
	Position               int                  `json:"position"`
	IsCurrentUser          bool                 `json:"isCurrentUser"`
	OfferedItemID          int64                `json:"offeredItemId"`
	OfferedItemTitle       string               `json:"offeredItemTitle"`
	OfferedItemDescription string               `json:"offeredItemDescription"`
	WantedDescription      string               `json:"wantedDescription"`
	ImageURL               *string              `json:"imageUrl,omitempty"`
	RequestStatus          entity.RequestStatus `json:"requestStatus"`
	Vote                   *entity.VoteValue    `json:"vote,omitempty"`
}

type exchangeOptionsResponse struct {
	ChainID              int64                    `json:"chainId"`
	Status               entity.ChainStatus       `json:"status"`
	Score                float64                  `json:"score"`
	Length               int                      `json:"length"`
	CurrentRequestID     int64                    `json:"currentRequestId"`
	CurrentPosition      int                      `json:"currentPosition"`
	GivesToPosition      int                      `json:"givesToPosition"`
	ReceivesFromPosition int                      `json:"receivesFromPosition"`
	CurrentOffer         exchangeOptionResponse   `json:"currentOffer"`
	ReceiveOptions       []exchangeOptionResponse `json:"receiveOptions"`
}

type exchangeOptionResponse struct {
	ClusterID         int64             `json:"clusterId"`
	RequestID         int64             `json:"requestId"`
	ItemID            int64             `json:"itemId"`
	Title             string            `json:"title"`
	Description       string            `json:"description"`
	WantedDescription string            `json:"wantedDescription"`
	ImageURL          *string           `json:"imageUrl,omitempty"`
	Vote              *entity.VoteValue `json:"vote,omitempty"`
}

type voteRequest struct {
	RequestID       int64 `json:"requestId" validate:"required,gt=0"`
	TargetRequestID int64 `json:"targetRequestId" validate:"required,gt=0,nefield=RequestID"`
}

type withdrawVoteQuery struct {
	RequestID       int64 `schema:"requestId" validate:"required,gt=0"`
	TargetRequestID int64 `schema:"targetRequestId" validate:"required,gt=0,nefield=RequestID"`
}

type voteResponse struct {
	ChainID         int64              `json:"chainId"`
	RequestID       int64              `json:"requestId"`
	TargetRequestID int64              `json:"targetRequestId"`
	Vote            entity.VoteValue   `json:"vote"`
	VotedAt         string             `json:"votedAt"`
	ChainStatus     entity.ChainStatus `json:"chainStatus"`
}

type selectReplacementRequest struct {
	RequestID int64 `json:"requestId" validate:"required,gt=0"`
}

type replacementResponse struct {
	RequestID         int64   `json:"requestId"`
	OfferedItemID     int64   `json:"offeredItemId"`
	Title             string  `json:"title"`
	Description       string  `json:"description"`
	WantedDescription string  `json:"wantedDescription"`
	ImageURL          *string `json:"imageUrl,omitempty"`
	Reliability       float64 `json:"reliability"`
	RespondedAt       string  `json:"respondedAt"`
}

type handoffRequest struct {
	ChainID   int64 `json:"chainId" validate:"required,gt=0"`
	RequestID int64 `json:"requestId" validate:"required,gt=0"`
}

type receiptRequest struct {
	RequestID int64 `json:"requestId" validate:"required,gt=0"`
}

// List возвращает актуальные цепочки аутентифицированного участника.
func (h *Handler) List(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	chains, err := h.service.List(c.Request.Context(), userID)
	if err != nil {
		api.SendError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	response := make([]chainResponse, 0, len(chains))
	for _, chain := range chains {
		response = append(response, newChainResponse(chain, userID))
	}
	api.SendOk(c, http.StatusOK, response)
}

// ExchangeOptions возвращает готовые варианты получения для конкретной заявки владельца.
func (h *Handler) ExchangeOptions(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	offerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || offerID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
		return
	}

	chains, err := h.service.ListForOffer(c.Request.Context(), userID, offerID)
	if err != nil {
		if errors.Is(err, entity.ErrExchangeOfferNotFound) {
			api.SendError(c, http.StatusNotFound, err.Error())
			return
		}
		api.SendError(c, http.StatusInternalServerError, "internal server error")
		return
	}

	response := make([]exchangeOptionsResponse, 0, len(chains))
	for _, chain := range chains {
		response = append(response, newExchangeOptionsResponse(chain))
	}
	api.SendOk(c, http.StatusOK, response)
}

// Get возвращает подробности цепочки только её участнику.
func (h *Handler) Get(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	chainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || chainID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
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
		api.SendError(c, http.StatusInternalServerError, "internal server error")
		return
	}
	api.SendOk(c, http.StatusOK, newChainResponse(chain, userID))
}

// Vote records the authenticated participant's response to one concrete item
// in the next position of a candidate chain.
func (h *Handler) Vote(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	chainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || chainID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
		return
	}

	var body voteRequest
	if err := validator.BindJSON(&body, c.Request); err != nil {
		var jsonSyntaxErr *json.SyntaxError
		if errors.As(err, &jsonSyntaxErr) {
			api.SendError(c, http.StatusBadRequest, "invalid JSON")
			return
		}
		api.SendError(c, http.StatusUnprocessableEntity, err.Error())
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
			api.SendError(c, http.StatusInternalServerError, "internal server error")
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

// WithdrawVote cancels a primary response before the chain enters PROPOSED.
func (h *Handler) WithdrawVote(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	chainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || chainID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
		return
	}
	var query withdrawVoteQuery
	if err := validator.BindQuery(&query, c.Request); err != nil {
		api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	err = h.service.WithdrawVote(c.Request.Context(), userID, chainID, chainservice.VoteInput{
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
			api.SendError(c, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	c.Status(http.StatusNoContent)
}

func newChainResponse(chain entity.Chain, userID string) chainResponse {
	response := chainResponse{
		ID:                   chain.ID,
		Status:               chain.Status,
		Score:                chain.Score,
		Length:               chain.Length,
		Version:              chain.Version,
		CurrentRequestID:     chain.CurrentRequestID,
		CurrentPosition:      chain.CurrentPosition,
		GivesToPosition:      cyclicPosition(chain.CurrentPosition-1, chain.Length),
		ReceivesFromPosition: cyclicPosition(chain.CurrentPosition+1, chain.Length),
		InvalidReason:        chain.InvalidReason,
		CreatedAt:            chain.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:            chain.UpdatedAt.UTC().Format(time.RFC3339),
		Participants:         make([]chainParticipantResponse, 0, len(chain.Participants)),
	}
	if chain.FreezeDeadlineAt != nil {
		value := chain.FreezeDeadlineAt.UTC().Format(time.RFC3339)
		response.FreezeDeadlineAt = &value
	}
	for _, participant := range chain.Participants {
		participantResponse := chainParticipantResponse{
			ClusterID:              participant.ClusterID,
			RequestID:              participant.RequestID,
			Position:               participant.Position,
			IsCurrentUser:          participant.OwnerUserID == userID,
			OfferedItemID:          participant.OfferedItemID,
			OfferedItemTitle:       participant.OfferedItemTitle,
			OfferedItemDescription: participant.OfferedItemDescription,
			WantedDescription:      participant.WantedDescription,
			ImageURL:               participant.ImageURL,
			RequestStatus:          participant.RequestStatus,
			Vote:                   participant.Vote,
		}
		response.Participants = append(response.Participants, participantResponse)
	}
	return response
}

func newExchangeOptionsResponse(chain entity.Chain) exchangeOptionsResponse {
	receivesFromPosition := cyclicPosition(chain.CurrentPosition+1, chain.Length)
	response := exchangeOptionsResponse{
		ChainID:              chain.ID,
		Status:               chain.Status,
		Score:                chain.Score,
		Length:               chain.Length,
		CurrentRequestID:     chain.CurrentRequestID,
		CurrentPosition:      chain.CurrentPosition,
		GivesToPosition:      cyclicPosition(chain.CurrentPosition-1, chain.Length),
		ReceivesFromPosition: receivesFromPosition,
		ReceiveOptions:       make([]exchangeOptionResponse, 0),
	}

	for _, participant := range chain.Participants {
		option := exchangeOptionResponse{
			ClusterID:         participant.ClusterID,
			RequestID:         participant.RequestID,
			ItemID:            participant.OfferedItemID,
			Title:             participant.OfferedItemTitle,
			Description:       participant.OfferedItemDescription,
			WantedDescription: participant.WantedDescription,
			ImageURL:          participant.ImageURL,
		}
		if participant.RequestID == chain.CurrentRequestID {
			response.CurrentOffer = option
		}
		if participant.Position == receivesFromPosition {
			option.Vote = participant.Vote
			response.ReceiveOptions = append(response.ReceiveOptions, option)
		}
	}
	return response
}

func cyclicPosition(position, length int) int {
	if length <= 0 {
		return 0
	}
	return (position%length + length) % length
}

func currentUserID(c *gin.Context) (string, bool) {
	userID, ok := middleware.UserID(c)
	if !ok {
		api.SendError(c, http.StatusUnauthorized, "authentication is required")
		return "", false
	}
	return userID.String(), true
}

// Confirm фиксирует подтверждение участия в PROPOSED-цепочке (раунд 2).
func (h *Handler) Confirm(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	chainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || chainID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
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
			api.SendError(c, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"chainId": chainID, "status": status})
}

// Think records an explicit decision to wait before confirming a proposal.
func (h *Handler) Think(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	chainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || chainID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
		return
	}
	if err := h.service.Think(c.Request.Context(), userID, chainID); err != nil {
		DetermineError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"chainId": chainID, "vote": entity.VoteThinking})
}

// Decline withdraws from a proposal and reports whether fast replacement is available.
func (h *Handler) Decline(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	chainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || chainID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
		return
	}
	replacementAvailable, status, err := h.service.Decline(c.Request.Context(), userID, chainID)
	if err != nil {
		DetermineError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"chainId": chainID, "status": status, "replacementAvailable": replacementAvailable,
	})
}

func (h *Handler) ListReplacements(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	chainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || chainID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
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
	c.JSON(http.StatusOK, response)
}

func (h *Handler) SelectReplacement(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	chainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || chainID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
		return
	}
	var body selectReplacementRequest
	if err := validator.BindJSON(&body, c.Request); err != nil {
		api.SendError(c, http.StatusUnprocessableEntity, err.Error())
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
	c.JSON(http.StatusOK, gin.H{"chainId": chainID, "requestId": body.RequestID, "status": entity.ChainStatusProposed})
}

// Handoff accepts the temporary local callback that an Avito delivery flow has
// handed over the item represented by the request.
func (h *Handler) Handoff(c *gin.Context) {
	var body handoffRequest
	if err := validator.BindJSON(&body, c.Request); err != nil {
		var jsonSyntaxErr *json.SyntaxError
		if errors.As(err, &jsonSyntaxErr) {
			api.SendError(c, http.StatusBadRequest, "invalid JSON")
			return
		}
		api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	result, err := h.service.Handoff(c.Request.Context(), body.ChainID, body.RequestID)
	if err != nil {
		DetermineError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ConfirmReceipt lets the physical recipient acknowledge one handed-off item.
func (h *Handler) ConfirmReceipt(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	chainID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || chainID <= 0 {
		api.SendError(c, http.StatusUnprocessableEntity, "id must be a positive integer")
		return
	}
	var body receiptRequest
	if err := validator.BindJSON(&body, c.Request); err != nil {
		var jsonSyntaxErr *json.SyntaxError
		if errors.As(err, &jsonSyntaxErr) {
			api.SendError(c, http.StatusBadRequest, "invalid JSON")
			return
		}
		api.SendError(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	result, err := h.service.ConfirmReceipt(c.Request.Context(), userID, chainID, body.RequestID)
	if err != nil {
		DetermineError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func DetermineError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, entity.ErrChainNotFound):
		api.SendError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, entity.ErrChainNotProposed):
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
		api.SendError(c, http.StatusInternalServerError, "internal server error")
	}
}
