package chain

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/sirupsen/logrus"
)

func (s *Service) ListReplacements(ctx context.Context, userID string, chainID int64) ([]entity.ReplacementOption, error) {
	if s.repository == nil {
		return nil, entity.ErrChainRepositoryNotConfigured
	}
	if chainID <= 0 {
		return nil, entity.ErrInvalidVoteTarget
	}
	if err := s.expireDue(ctx); err != nil {
		return nil, err
	}
	return s.repository.ListReplacementOptions(ctx, userID, chainID)
}

func (s *Service) SelectReplacement(ctx context.Context, userID string, chainID, replacementRequestID int64) error {
	if s.repository == nil || s.transactions == nil {
		return entity.ErrChainRepositoryNotConfigured
	}
	if chainID <= 0 || replacementRequestID <= 0 {
		return entity.ErrInvalidVoteTarget
	}
	if err := s.expireDue(ctx); err != nil {
		return err
	}
	var proposalExpired bool
	var replacementSelected bool
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
		if err := s.repository.SelectReplacement(ctx, tx, userID, chainID, replacementRequestID); err != nil {
			return err
		}
		replacementSelected = true
		return nil
	})
	if err != nil {
		return err
	}
	if proposalExpired {
		return entity.ErrChainConfirmationExpired
	}
	if replacementSelected && s.notifier != nil {
		if notifyErr := s.notifier.NotifyReplacementInvited(ctx, chainID, replacementRequestID); notifyErr != nil {
			logrus.WithError(notifyErr).WithFields(logrus.Fields{
				"chain_id":   chainID,
				"request_id": replacementRequestID,
			}).Error("failed to send replacement invitation")
		}
	}
	return nil
}

// Handoff records an external confirmation that the item from requestID was
// handed to the participant at the previous position in the frozen ring.
