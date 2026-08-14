package cluster

import (
	"context"
	"errors"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/repository"
	clusterservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/cluster"
	"github.com/jackc/pgx/v5"
)

func (r *Postgres) LoadVectors(ctx context.Context, tx database.Tx, offerID int64) (clusterservice.OfferVectors, error) {
	var offerEmbedding *string
	var wantEmbedding *string
	var category string
	var wantedCategory string
	err := tx.QueryRow(ctx, `
		SELECT i.embedding::text,
		       eo.want_embedding::text,
		       COALESCE(i.category, ''),
		       COALESCE(eo.wanted_category, '')
		FROM exchange_offers AS eo
		JOIN items AS i ON i.id = eo.offered_item_id
		WHERE eo.id = $1 AND eo.status = 'ACTIVE'
	`, offerID).Scan(&offerEmbedding, &wantEmbedding, &category, &wantedCategory)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			if mappedErr, ok := repository.DBErrToErr(err); ok {
				return clusterservice.OfferVectors{}, mappedErr
			}
			return clusterservice.OfferVectors{}, err
		}
		return clusterservice.OfferVectors{}, entity.ErrExchangeOfferNotFound
	}
	if offerEmbedding == nil || wantEmbedding == nil {
		return clusterservice.OfferVectors{}, entity.ErrOfferEmbeddingMissing
	}
	return clusterservice.OfferVectors{
		OfferEmbedding: *offerEmbedding,
		WantEmbedding:  *wantEmbedding,
		Category:       category,
		WantedCategory: wantedCategory,
	}, nil
}
