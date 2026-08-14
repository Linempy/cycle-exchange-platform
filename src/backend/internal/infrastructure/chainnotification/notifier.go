// Package chainnotification delivers chain events to the owners of participating offers.
package chainnotification

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Sender interface {
	SendChainFrozen(to string, chainID int64, gives, receives string) error
	SendReplacementInvitation(to string, chainID int64, gives, receives string) error
}

type Notifier struct {
	pool   *pgxpool.Pool
	sender Sender
}

func New(pool *pgxpool.Pool, sender Sender) *Notifier {
	return &Notifier{pool: pool, sender: sender}
}

func (n *Notifier) NotifyChainFrozen(ctx context.Context, chainID int64) error {
	rows, err := n.pool.Query(ctx, `
		SELECT u.email, own_item.title, received_item.title
		FROM chain_participants AS cp
		JOIN chains AS chain ON chain.id = cp.chain_id
		JOIN exchange_offers AS eo ON eo.id = cp.request_id
		JOIN users AS u ON u.id = eo.user_id
		JOIN items AS own_item ON own_item.id = eo.offered_item_id
		JOIN chain_participants AS next_cp
		  ON next_cp.chain_id = cp.chain_id
		 AND next_cp.position = (cp.position + 1) % chain.length
		JOIN exchange_offers AS next_offer ON next_offer.id = next_cp.request_id
		JOIN items AS received_item ON received_item.id = next_offer.offered_item_id
		WHERE cp.chain_id = $1
		ORDER BY cp.position`, chainID)
	if err != nil {
		return fmt.Errorf("load frozen chain recipients: %w", err)
	}
	defer rows.Close()

	var sendErrors []error
	for rows.Next() {
		var email, gives, receives string
		if err := rows.Scan(&email, &gives, &receives); err != nil {
			return fmt.Errorf("scan frozen chain recipient: %w", err)
		}
		if err := n.sender.SendChainFrozen(email, chainID, gives, receives); err != nil {
			sendErrors = append(sendErrors, fmt.Errorf("send frozen chain notification to %s: %w", email, err))
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate frozen chain recipients: %w", err)
	}
	return errors.Join(sendErrors...)
}

func (n *Notifier) NotifyReplacementInvited(ctx context.Context, chainID, requestID int64) error {
	var email, gives, receives string
	err := n.pool.QueryRow(ctx, `
		SELECT u.email, own_item.title, received_item.title
		FROM chain_participants AS cp
		JOIN chains AS chain ON chain.id = cp.chain_id
		JOIN exchange_offers AS eo ON eo.id = cp.request_id
		JOIN users AS u ON u.id = eo.user_id
		JOIN items AS own_item ON own_item.id = eo.offered_item_id
		JOIN chain_participants AS next_cp
		  ON next_cp.chain_id = cp.chain_id
		 AND next_cp.position = (cp.position + 1) % chain.length
		JOIN exchange_offers AS next_offer ON next_offer.id = next_cp.request_id
		JOIN items AS received_item ON received_item.id = next_offer.offered_item_id
		WHERE cp.chain_id = $1 AND cp.request_id = $2`, chainID, requestID).Scan(&email, &gives, &receives)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("replacement request %d is not pinned to chain %d", requestID, chainID)
	}
	if err != nil {
		return fmt.Errorf("load replacement recipient: %w", err)
	}
	if err := n.sender.SendReplacementInvitation(email, chainID, gives, receives); err != nil {
		return fmt.Errorf("send replacement invitation to %s: %w", email, err)
	}
	return nil
}
