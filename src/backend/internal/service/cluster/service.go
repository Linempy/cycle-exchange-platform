// Package cluster реализует правила актуализации кластеров предложений.
package cluster

import (
	"context"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
)

const (
	defaultTopK            = 50
	defaultThreshold       = 0.8
	defaultDirectionMargin = 0.05
)

type Service struct {
	repository      Repository
	searcher        CandidateSearcher
	topK            int
	threshold       float64
	directionMargin float64
}

func NewService(repository Repository, searcher CandidateSearcher, topK int, threshold, directionMargin float64) *Service {
	if topK <= 0 {
		topK = defaultTopK
	}
	if threshold <= 0 || threshold > 1 {
		threshold = defaultThreshold
	}
	if directionMargin < 0 || directionMargin > 1 {
		directionMargin = defaultDirectionMargin
	}
	return &Service{
		repository:      repository,
		searcher:        searcher,
		topK:            topK,
		threshold:       threshold,
		directionMargin: directionMargin,
	}
}

// Synchronize — сменить membership; advisory xact lock сериализует перестройку кластеров.
func (s *Service) Synchronize(ctx context.Context, tx database.Tx, offerID int64) error {
	if s.repository == nil || s.searcher == nil {
		return entity.ErrClusterNotConfigured
	}
	// Cluster membership is derived from the currently committed neighbourhood.
	// Serialize synchronization so two equal requests cannot both observe an
	// empty neighbourhood and create separate singleton clusters.
	if tx != nil {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742913005)`); err != nil {
			return err
		}
	}

	vectors, err := s.repository.LoadVectors(ctx, tx, offerID)
	if err != nil {
		return err
	}

	oldClusterID, err := s.repository.DeleteMembership(ctx, tx, offerID)
	if err != nil {
		return err
	}
	if oldClusterID != nil {
		if err := s.repository.Refresh(ctx, tx, *oldClusterID); err != nil {
			return err
		}
	}

	candidates, err := s.searcher.FindSimilarOffers(
		ctx,
		vectors.OfferEmbedding,
		vectors.WantEmbedding,
		vectors.Category,
		vectors.WantedCategory,
		offerID,
		s.threshold,
		s.directionMargin,
		s.topK,
	)
	if err != nil {
		return err
	}

	candidateIDs := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.RequestID)
	}

	clusterID, err := s.repository.FindClusterForCandidates(
		ctx, tx, candidateIDs, vectors, s.threshold, s.directionMargin,
	)
	if err != nil {
		return err
	}
	if clusterID == nil {
		createdID, err := s.repository.Create(ctx, tx)
		if err != nil {
			return err
		}
		clusterID = &createdID
	} else if err := s.repository.ConsolidateCandidateClusters(
		ctx, tx, *clusterID, candidateIDs, vectors, s.threshold, s.directionMargin,
	); err != nil {
		return err
	}

	if err := s.repository.AddMember(ctx, tx, *clusterID, offerID); err != nil {
		return err
	}
	return s.repository.Refresh(ctx, tx, *clusterID)
}

// Remove исключает предложение из кластера и обновляет либо удаляет кластер.
func (s *Service) Remove(ctx context.Context, tx database.Tx, offerID int64) error {
	if s.repository == nil {
		return entity.ErrClusterNotConfigured
	}

	clusterID, err := s.repository.DeleteMembership(ctx, tx, offerID)
	if err != nil || clusterID == nil {
		return err
	}
	return s.repository.Refresh(ctx, tx, *clusterID)
}

func (s *Service) ListActiveMembers(ctx context.Context, clusterID int64) ([]entity.ExchangeOffer, error) {
	return s.repository.ListActiveMembers(ctx, clusterID)
}
