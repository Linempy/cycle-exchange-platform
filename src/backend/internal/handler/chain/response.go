package chain

import (
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

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
