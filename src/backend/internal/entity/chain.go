package entity

import "time"

// ChainStatus описывает этап жизненного цикла цепочки обмена.
type ChainStatus string

const (
	ChainStatusCandidate  ChainStatus = "CANDIDATE"
	ChainStatusProposed   ChainStatus = "PROPOSED"
	ChainStatusFrozen     ChainStatus = "FROZEN"
	ChainStatusInProgress ChainStatus = "IN_PROGRESS"
	ChainStatusCompleted  ChainStatus = "COMPLETED"
	ChainStatusBroken     ChainStatus = "BROKEN"
)

// Chain содержит цепочку кластеров и доступные заявки в каждой её позиции.
type Chain struct {
	ID               int64
	Status           ChainStatus
	Score            float64
	Length           int
	FreezeDeadlineAt *time.Time
	InvalidReason    *string
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CurrentRequestID int64
	CurrentPosition  int
	Participants     []ChainParticipant
}

// ChainParticipant описывает доступную заявку внутри кластера на позиции цикла.
// На одной позиции может быть несколько заявок одного кластера. Участник получает
// товар из следующей позиции и отдаёт свой товар предыдущей позиции по кругу.
type ChainParticipant struct {
	ID                     int64
	ChainID                int64
	ClusterID              int64
	RequestID              int64
	Position               int
	OwnerUserID            string
	OfferedItemID          int64
	OfferedItemTitle       string
	OfferedItemDescription string
	WantedDescription      string
	ImageURL               *string
	RequestStatus          RequestStatus
	Vote                   *VoteValue
	CreatedAt              time.Time
}

// ReplacementOption — ACTIVE из кластера отказавшегося; замена без пересборки цикла.
type ReplacementOption struct {
	RequestID         int64
	OfferedItemID     int64
	Title             string
	Description       string
	WantedDescription string
	ImageURL          *string
	Reliability       float64
	RespondedAt       time.Time
}
