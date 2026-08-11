package chain

import (
	"context"
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

// Repository описывает хранилище, необходимое сервису цепочек.
type Repository interface {
	SaveCandidates(ctx context.Context, tx database.Tx, drafts []entity.ChainDraft) error
	List(ctx context.Context, userID string) ([]entity.Chain, error)
	ListForOffer(ctx context.Context, userID string, offerID int64) ([]entity.Chain, error)
	Get(ctx context.Context, userID string, chainID int64) (entity.Chain, error)
	LockForVote(ctx context.Context, tx database.Tx, chainID int64) (entity.ChainStatus, int, error)
	ValidateVoteParticipants(ctx context.Context, tx database.Tx, userID string, chainID, requestID, targetRequestID int64, chainLength int) error
	GetVote(ctx context.Context, tx database.Tx, userID string, chainID, requestID, targetRequestID int64) (entity.ChainVote, error)
	UpsertPendingVote(ctx context.Context, tx database.Tx, chainID, requestID, targetRequestID int64) (time.Time, error)
	DeletePendingVote(ctx context.Context, tx database.Tx, chainID, requestID, targetRequestID int64) error
	ListPendingVoteEdges(ctx context.Context, tx database.Tx, chainID int64) ([]entity.VoteEdge, error)
	Propose(ctx context.Context, tx database.Tx, chainID int64, requestIDsByPosition []int64, confirmationDeadline time.Time) error
	ExpireProposalIfDue(ctx context.Context, tx database.Tx, chainID int64) (bool, error)
	MarkRequestInProposal(ctx context.Context, tx database.Tx, requestID int64) error
	RestoreActiveIfNoPendingVotes(ctx context.Context, tx database.Tx, requestID int64) error
	LoadScoreFeatures(ctx context.Context, tx database.Tx, chainID int64) (cosines []float64, reliability []float64, sizes []int, err error)
	CountPendingVoters(ctx context.Context, tx database.Tx, chainID int64) (int, error)
	UpdateScore(ctx context.Context, tx database.Tx, chainID int64, score float64) error
	ConfirmParticipant(ctx context.Context, tx database.Tx, chainID, requestID, targetRequestID int64) error
	MarkParticipantThinking(ctx context.Context, tx database.Tx, chainID, requestID, targetRequestID int64) error
	DeclineParticipant(ctx context.Context, tx database.Tx, chainID, requestID int64, fastReplacementEligible bool) (bool, entity.ChainStatus, error)
	ListReplacementOptions(ctx context.Context, userID string, chainID int64) ([]entity.ReplacementOption, error)
	SelectReplacement(ctx context.Context, tx database.Tx, userID string, chainID, replacementRequestID int64) error
	CountApprovedVoters(ctx context.Context, tx database.Tx, chainID int64) (int, error)
	CountApprovedVotersExcept(ctx context.Context, tx database.Tx, chainID, requestID int64) (int, error)
	MarkRequestLocked(ctx context.Context, tx database.Tx, requestID int64) error
	FreezeChain(ctx context.Context, tx database.Tx, chainID int64, deadline time.Time) error
	LockRequestsInChain(ctx context.Context, tx database.Tx, chainID int64) error
	MarkItemsUnavailable(ctx context.Context, tx database.Tx, chainID int64) error
	LoadChainRequestIDs(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error)
	LockRequestsForFreeze(ctx context.Context, tx database.Tx, requestIDs []int64) error
	LoadRequestLiveChainStatus(ctx context.Context, tx database.Tx, requestID int64) (entity.ChainStatus, error)
	FindParticipantEdge(ctx context.Context, tx database.Tx, chainID int64, userID string) (requestID, targetRequestID int64, err error)
	ReleaseUnselectedFromChain(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error)
	MarkRequestInProgress(ctx context.Context, tx database.Tx, chainID, requestID int64) (entity.RequestStatus, error)
	StartChain(ctx context.Context, tx database.Tx, chainID int64) error
	FindReceiptRequestStatus(ctx context.Context, tx database.Tx, chainID, requestID int64, userID string) (entity.RequestStatus, error)
	MarkRequestDone(ctx context.Context, tx database.Tx, requestID int64) error
	AllChainRequestsDone(ctx context.Context, tx database.Tx, chainID int64) (bool, error)
	CompleteChain(ctx context.Context, tx database.Tx, chainID int64) error

	// Жизненный цикл цепочек (пересборка/удаление) под властью matcher.
	ListChainsContainingRequest(ctx context.Context, tx database.Tx, requestID int64) ([]int64, error)
	DeleteRequestParticipation(ctx context.Context, tx database.Tx, requestID int64) error
	DeleteChain(ctx context.Context, tx database.Tx, chainID int64) error
	// ReleaseCompetitorsFromOtherChains вычёркивает участников замороженной chainID
	// из конкурирующих цепочек и возвращает их chainID для пересборки матчером.
	ReleaseCompetitorsFromOtherChains(ctx context.Context, tx database.Tx, chainID int64) ([]int64, error)
}

// Notifier отправляет пользовательские уведомления о событиях цепочки
// (например, письма о замыкании цикла). Подключается явно; до подключения
// сервис работает без уведомлений.
type Notifier interface {
	NotifyChainProposed(ctx context.Context, chainID int64, participants []entity.ChainParticipant) error
}
