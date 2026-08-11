package matching_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/matching"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/utils/ranker"
)

func TestFacadeSynchronizesAndRemovesClusterMembership(t *testing.T) {
	clusters := &fakeClusters{}
	cycles := &fakeCycles{clusters: clusters}
	chains := &fakeChains{cycles: cycles}
	ranker := ranker.NewChainScoreCalculator(ranker.NewRankerConfig())
	facade := matching.NewFacade(clusters, cycles, chains).WithRanker(ranker)

	drafts, err := facade.RebuildForRequest(context.Background(), nil, 11)
	if err != nil {
		t.Fatalf("RebuildForRequest() error = %v", err)
	}
	// Score считает Ranker один раз из фич драфта (дл. 2, рёбер/размеров нет):
	// Reliability=0.75 -> wR * 0.75 = 0.20*0.75 = 0.15 (шкала [0,1]).
	const wantScore = 0.15
	if len(drafts) != 1 || drafts[0].Score != wantScore {
		t.Fatalf("RebuildForRequest() drafts = %#v, want score %v", drafts, wantScore)
	}
	if len(chains.saved) != 1 || chains.saved[0].Score != wantScore {
		t.Fatalf("saved drafts = %#v, want score %v", chains.saved, wantScore)
	}
	if err := facade.RemoveRequest(context.Background(), nil, 11); err != nil {
		t.Fatalf("RemoveRequest() error = %v", err)
	}
	if clusters.synchronizedID != 11 || clusters.removedID != 11 || cycles.searchedID != 11 {
		t.Fatalf("cluster calls = synchronize %d, remove %d", clusters.synchronizedID, clusters.removedID)
	}
}

func TestFacadePropagatesClusterError(t *testing.T) {
	wantErr := errors.New("cluster failed")
	cycles := &fakeCycles{}
	facade := matching.NewFacade(&fakeClusters{err: wantErr}, cycles, &fakeChains{})

	_, err := facade.RebuildForRequest(context.Background(), nil, 11)
	if !errors.Is(err, wantErr) {
		t.Fatalf("RebuildForRequest() error = %v, want %v", err, wantErr)
	}
	if cycles.searchedID != 0 {
		t.Fatal("cycle search must not run when cluster synchronization fails")
	}
}

func TestRebuildForRequestDeletesAffectedChainsBeforeSavingFreshCandidates(t *testing.T) {
	clusters := &fakeClusters{}
	cycles := &fakeCycles{clusters: clusters}
	chains := &fakeChains{
		cycles: cycles,
		affectedByRequest: map[int64][]int64{
			11: {7, 8},
		},
		requestIDsByChain: map[int64][]int64{
			7: {11, 20},
			8: {30, 11},
		},
	}
	facade := matching.NewFacade(clusters, cycles, chains).
		WithRanker(ranker.NewChainScoreCalculator(ranker.NewRankerConfig()))

	if _, err := facade.RebuildForRequest(context.Background(), nil, 11); err != nil {
		t.Fatalf("RebuildForRequest() error = %v", err)
	}
	if len(chains.deleted) != 2 || chains.deleted[0] != 7 || chains.deleted[1] != 8 {
		t.Fatalf("deleted chains = %v, want [7 8]", chains.deleted)
	}
	if len(cycles.searchedIDs) != 3 || cycles.searchedIDs[0] != 11 || cycles.searchedIDs[1] != 20 || cycles.searchedIDs[2] != 30 {
		t.Fatalf("searched requests = %v, want [11 20 30]", cycles.searchedIDs)
	}
}

func TestCandidateValidatorFiltersThresholdOwnerAndDuplicates(t *testing.T) {
	validator := matching.NewCandidateValidator(0.8)
	candidates := []entity.Candidate{
		{RequestID: 1, OwnerID: "other", Score: 0.95},
		{RequestID: 1, OwnerID: "other", Score: 0.90},
		{RequestID: 2, OwnerID: "me", Score: 0.99},
		{RequestID: 3, OwnerID: "other", Score: 0.79},
	}

	result := validator.Validate(context.Background(), candidates, "me")
	if len(result) != 1 || result[0].RequestID != 1 {
		t.Fatalf("Validate() = %#v, want only request 1", result)
	}
}

func TestRepairAffectedChainsDeletesOldVariantsAndRebuildsRemaining(t *testing.T) {
	clusters := &fakeClusters{}
	cycles := &fakeCycles{clusters: clusters}
	chains := &fakeChains{
		cycles: cycles,
		requestIDsByChain: map[int64][]int64{
			7: {20, 30},
			8: {},
		},
	}
	facade := matching.NewFacade(clusters, cycles, chains).
		WithRanker(ranker.NewChainScoreCalculator(ranker.NewRankerConfig()))

	if err := facade.RepairAffectedChains(context.Background(), nil, []int64{7, 8}); err != nil {
		t.Fatalf("RepairAffectedChains() error = %v", err)
	}
	if len(chains.deleted) != 2 || chains.deleted[0] != 7 || chains.deleted[1] != 8 {
		t.Fatalf("deleted chains = %v, want [7 8]", chains.deleted)
	}
	if cycles.searchedID != 20 {
		t.Fatalf("rebuilt request = %d, want 20", cycles.searchedID)
	}
}

func TestRebuildRequestsSkipsDuplicatesAndInvalidIDs(t *testing.T) {
	clusters := &fakeClusters{}
	cycles := &fakeCycles{clusters: clusters}
	facade := matching.NewFacade(clusters, cycles, &fakeChains{cycles: cycles}).
		WithRanker(ranker.NewChainScoreCalculator(ranker.NewRankerConfig()))

	if err := facade.RebuildRequests(context.Background(), nil, []int64{0, 20, 20}); err != nil {
		t.Fatalf("RebuildRequests() error = %v", err)
	}
	if clusters.synchronizeCalls != 1 || cycles.searchedID != 20 {
		t.Fatalf("rebuild calls = %d, searched request = %d; want 1 and 20", clusters.synchronizeCalls, cycles.searchedID)
	}
}

type fakeClusters struct {
	synchronizedID   int64
	synchronizeCalls int
	removedID        int64
	err              error
}

type fakeCycles struct {
	clusters    *fakeClusters
	searchedID  int64
	searchedIDs []int64
}

type fakeChains struct {
	cycles            *fakeCycles
	saved             []entity.ChainDraft
	requestIDsByChain map[int64][]int64
	affectedByRequest map[int64][]int64
	deleted           []int64
}

func (c *fakeChains) SaveCandidates(_ context.Context, _ database.Tx, drafts []entity.ChainDraft) error {
	if c.cycles != nil && c.cycles.searchedID == 0 {
		return errors.New("chain saving started before cycle search")
	}
	c.saved = append(c.saved, drafts...)
	return nil
}
func (c *fakeChains) ListChainsContainingRequest(_ context.Context, _ database.Tx, requestID int64) ([]int64, error) {
	return c.affectedByRequest[requestID], nil
}
func (c *fakeChains) LoadChainRequestIDs(_ context.Context, _ database.Tx, chainID int64) ([]int64, error) {
	return c.requestIDsByChain[chainID], nil
}
func (c *fakeChains) DeleteRequestParticipation(_ context.Context, _ database.Tx, _ int64) error {
	return nil
}

func (c *fakeChains) DeleteChain(_ context.Context, _ database.Tx, chainID int64) error {
	c.deleted = append(c.deleted, chainID)
	return nil
}

func (c *fakeCycles) Find(_ context.Context, _ database.Tx, requestID int64) ([]entity.ChainDraft, error) {
	if c.clusters != nil && c.clusters.synchronizeCalls == 0 {
		return nil, errors.New("cycle search started before cluster synchronization")
	}
	c.searchedID = requestID
	c.searchedIDs = append(c.searchedIDs, requestID)
	// Драфт с 2 участниками, чтобы ChainState.Count >= 2 (иначе Ranker вернёт
	// ErrInvalidChainState). Score вычисляет уже сам фасад через Ranker.
	return []entity.ChainDraft{{
		Participants: []entity.ChainDraftParticipant{
			{ClusterID: 101, RequestID: 11},
			{ClusterID: 102, RequestID: 12},
		},
	}}, nil
}

func (c *fakeClusters) Synchronize(_ context.Context, _ database.Tx, offerID int64) error {
	c.synchronizedID = offerID
	c.synchronizeCalls++
	return c.err
}

func (c *fakeClusters) Remove(_ context.Context, _ database.Tx, offerID int64) error {
	c.removedID = offerID
	return c.err
}
