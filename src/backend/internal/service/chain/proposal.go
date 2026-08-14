package chain

import (
	"context"
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/utils/ranker"
	"github.com/sirupsen/logrus"
)

func (s *Service) Confirm(ctx context.Context, userID string, chainID int64) (entity.ChainStatus, error) {
	if s.repository == nil || s.transactions == nil {
		return entity.ChainStatus(""), entity.ErrChainRepositoryNotConfigured
	}
	if chainID <= 0 {
		return entity.ChainStatus(""), entity.ErrInvalidVoteTarget
	}
	if err := s.expireDue(ctx); err != nil {
		return entity.ChainStatus(""), err
	}

	var resultStatus entity.ChainStatus
	var proposalExpired bool
	var frozenNow bool
	err := s.transactions.WithinTransaction(ctx, func(tx database.Tx) error {
		status, length, err := s.repository.LockForVote(ctx, tx, chainID)
		if err != nil {
			return err
		}
		expired, err := s.repository.ExpireProposalIfDue(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if expired {
			proposalExpired = true
			return nil
		}

		// Идемпотентный возврат: если цепочка уже заморожена — успех.
		if status != entity.ChainStatusProposed {
			if status == entity.ChainStatusFrozen {
				if _, _, err := s.repository.FindParticipantEdge(ctx, tx, chainID, userID); err != nil {
					return err
				}
				resultStatus = status
				return nil
			}
			return entity.ErrChainNotProposed
		}

		requestIDs, err := s.repository.LoadChainRequestIDs(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if err := s.repository.LockRequestsForFreeze(ctx, tx, requestIDs); err != nil {
			return err
		}

		requestID, targetID, err := s.repository.FindParticipantEdge(ctx, tx, chainID, userID)
		if err != nil {
			return err
		}

		if err := s.repository.ConfirmParticipant(ctx, tx, chainID, requestID, targetID); err != nil {
			return err
		}
		if err := s.repository.MarkRequestLocked(ctx, tx, requestID); err != nil {
			return err
		}

		approved, err := s.repository.CountApprovedVoters(ctx, tx, chainID)
		if err != nil {
			return err
		}

		if approved < length {
			resultStatus = entity.ChainStatusProposed
			return s.refreshScore(ctx, tx, chainID, entity.ChainStatusProposed, ranker.EventConfirm)
		}

		// Все подтвердили — замораживаем в той же транзакции.
		if s.freezer == nil {
			return entity.ErrChainRepositoryNotConfigured
		}
		if err := s.freezer.Freeze(ctx, tx, chainID); err != nil {
			return err
		}
		frozenNow = true
		resultStatus = entity.ChainStatusFrozen
		return s.refreshScore(ctx, tx, chainID, entity.ChainStatusFrozen, ranker.EventConfirm)
	})
	if err != nil {
		return entity.ChainStatus(""), err
	}
	if proposalExpired {
		return entity.ChainStatus(""), entity.ErrChainConfirmationExpired
	}
	if frozenNow && s.notifier != nil {
		if notifyErr := s.notifier.NotifyChainFrozen(ctx, chainID); notifyErr != nil {
			logrus.WithError(notifyErr).WithField("chain_id", chainID).
				Error("failed to send frozen chain notifications")
		}
	}
	return resultStatus, nil
}

// Unconfirm withdraws a round-two approval while the chain is still waiting
// for confirmations. The participant stays in the proposal and may confirm
// again. During a fast-replacement round any additional withdrawal cancels
// the round for everyone instead of leaving several simultaneous vacancies.
func (s *Service) Unconfirm(ctx context.Context, userID string, chainID int64) (entity.ChainStatus, error) {
	if s.repository == nil || s.transactions == nil {
		return "", entity.ErrChainRepositoryNotConfigured
	}
	if chainID <= 0 {
		return "", entity.ErrInvalidVoteTarget
	}
	result := entity.ChainStatusProposed
	var expired bool
	err := s.transactions.WithinTransaction(ctx, func(tx database.Tx) error {
		status, _, err := s.repository.LockForVote(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if status != entity.ChainStatusProposed {
			return entity.ErrChainNotProposed
		}
		expired, err = s.repository.ExpireProposalIfDue(ctx, tx, chainID)
		if err != nil || expired {
			return err
		}
		requestID, targetID, err := s.repository.FindParticipantEdge(ctx, tx, chainID, userID)
		if err != nil {
			return err
		}
		replacing, err := s.repository.IsFrozenReplacement(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if replacing {
			_, result, err = s.repository.DeclineParticipant(ctx, tx, chainID, requestID, false)
			return err
		}
		if err := s.repository.UnconfirmParticipant(ctx, tx, chainID, requestID, targetID); err != nil {
			return err
		}
		return s.refreshScore(ctx, tx, chainID, entity.ChainStatusProposed, ranker.EventRespond)
	})
	if err != nil {
		return "", err
	}
	if expired {
		return "", entity.ErrChainConfirmationExpired
	}
	return result, nil
}

// Think marks an explicit decision to postpone confirmation. Pending remains
// reserved for a participant who has not made a round-two decision yet.

func (s *Service) Think(ctx context.Context, userID string, chainID int64) error {
	if s.repository == nil || s.transactions == nil {
		return entity.ErrChainRepositoryNotConfigured
	}
	if chainID <= 0 {
		return entity.ErrInvalidVoteTarget
	}
	if err := s.expireDue(ctx); err != nil {
		return err
	}
	var proposalExpired bool
	err := s.transactions.WithinTransaction(ctx, func(tx database.Tx) error {
		status, _, err := s.repository.LockForVote(ctx, tx, chainID)
		if err != nil {
			return err
		}
		expired, err := s.repository.ExpireProposalIfDue(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if expired {
			proposalExpired = true
			return nil
		}
		if status != entity.ChainStatusProposed {
			return entity.ErrChainNotProposed
		}
		requestID, targetID, err := s.repository.FindParticipantEdge(ctx, tx, chainID, userID)
		if err != nil {
			return err
		}
		return s.repository.MarkParticipantThinking(ctx, tx, chainID, requestID, targetID)
	})
	if err != nil {
		return err
	}
	if proposalExpired {
		return entity.ErrChainConfirmationExpired
	}
	return nil
}

// Decline releases the participant's request. Fast replacement is allowed only
// when all other participants have already confirmed. An earlier refusal rolls
// the proposal back to CANDIDATE and resets confirmations to pending.

func (s *Service) Decline(ctx context.Context, userID string, chainID int64) (bool, entity.ChainStatus, error) {
	if s.repository == nil || s.transactions == nil {
		return false, "", entity.ErrChainRepositoryNotConfigured
	}
	if chainID <= 0 {
		return false, "", entity.ErrInvalidVoteTarget
	}
	if err := s.expireDue(ctx); err != nil {
		return false, "", err
	}
	var replacementAvailable bool
	resultStatus := entity.ChainStatusCandidate
	var proposalExpired bool
	err := s.transactions.WithinTransaction(ctx, func(tx database.Tx) error {
		status, chainLength, err := s.repository.LockForVote(ctx, tx, chainID)
		if err != nil {
			return err
		}
		expired, err := s.repository.ExpireProposalIfDue(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if expired {
			proposalExpired = true
			return nil
		}
		if status != entity.ChainStatusProposed && status != entity.ChainStatusFrozen {
			return entity.ErrChainNotProposed
		}
		requestID, _, err := s.repository.FindParticipantEdge(ctx, tx, chainID, userID)
		if err != nil {
			return err
		}
		frozenReplacement, err := s.repository.IsFrozenReplacement(ctx, tx, chainID)
		if err != nil {
			return err
		}
		if status == entity.ChainStatusFrozen {
			if err := s.repository.PrepareFrozenReplacement(ctx, tx, chainID, time.Now().Add(replacementTTL)); err != nil {
				return err
			}
			frozenReplacement = false
		}
		approved, err := s.repository.CountApprovedVotersExcept(ctx, tx, chainID, requestID)
		if err != nil {
			return err
		}
		fastReplacementEligible := !frozenReplacement && approved == chainLength-1
		replacementAvailable, resultStatus, err = s.repository.DeclineParticipant(ctx, tx, chainID, requestID, fastReplacementEligible)
		if err != nil {
			return err
		}
		return nil
	})
	if proposalExpired && err == nil {
		err = entity.ErrChainConfirmationExpired
	}
	return replacementAvailable, resultStatus, err
}
