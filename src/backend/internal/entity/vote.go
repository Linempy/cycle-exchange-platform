package entity

import "time"

// VoteValue is a participant response to a concrete exchange option.
type VoteValue string

const (
	VotePending  VoteValue = "pending"
	VoteApproved VoteValue = "approved"
	VoteThinking VoteValue = "thinking"
	VoteRejected VoteValue = "rejected"
)

// ChainVote — направленный отклик request -> target внутри цепочки.
type ChainVote struct {
	ChainID         int64
	RequestID       int64
	TargetRequestID int64
	Vote            VoteValue
	VotedAt         time.Time
	ChainStatus     ChainStatus
}

// VoteEdge is an approved directed edge used to find a closed response cycle.
type VoteEdge struct {
	RequestID       int64
	TargetRequestID int64
	Position        int
}
