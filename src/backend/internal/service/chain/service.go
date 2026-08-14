package chain

import (
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/utils/ranker"
)

const (
	minClusters     = 2
	maxClusters     = 5
	confirmationTTL = 24 * time.Hour
	replacementTTL  = 3 * time.Hour
)

// Service сохраняет найденные варианты цепочек и выдаёт доступные пользователю цепочки.
type Service struct {
	repository   Repository
	transactions database.TransactionManager
	scorer       ranker.Ranker
	notifier     Notifier
	freezer      *FreezeService
}

// NewService создаёт сервис цепочек.
func NewService(repository Repository, transactions database.TransactionManager) *Service {
	return &Service{repository: repository, transactions: transactions}
}

func (s *Service) WithFreezer(freezer *FreezeService) *Service {
	s.freezer = freezer
	return s
}

// WithScorer подключает пересчёт score при откликах/отказах.
func (s *Service) WithScorer(scorer ranker.Ranker) *Service {
	s.scorer = scorer
	return s
}

// WithNotifier подключает отправку уведомлений о событиях цепочки.
func (s *Service) WithNotifier(notifier Notifier) *Service {
	s.notifier = notifier
	return s
}

// VoteInput identifies one directed response inside a candidate chain.
type VoteInput struct {
	RequestID       int64 `json:"requestId" validate:"required,gt=0"`
	TargetRequestID int64 `json:"targetRequestId" validate:"required,gt=0,nefield=RequestID"`
}

// FulfillmentResult reports the aggregate state after a handoff or receipt.
type FulfillmentResult struct {
	ChainID   int64              `json:"chainId"`
	RequestID int64              `json:"requestId"`
	Status    entity.ChainStatus `json:"status"`
}
