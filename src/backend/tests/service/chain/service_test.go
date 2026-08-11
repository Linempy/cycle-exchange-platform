package chain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/core/database"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	chainservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/chain"
	"github.com/Avito-Team-Not-Found/tricky-exchanger/pkg/utils/ranker"
)

func TestSaveCandidatesCanonicalizesCycleRotation(t *testing.T) {
	repository := &fakeRepository{}
	service := chainservice.NewService(repository, fakeTransactionManager{})
	draft := entity.ChainDraft{
		Score: 0.9,
		Participants: []entity.ChainDraftParticipant{
			{ClusterID: 30, RequestID: 1},
			{ClusterID: 10, RequestID: 99},
			{ClusterID: 20, RequestID: 2},
		},
	}

	if err := service.SaveCandidates(context.Background(), nil, []entity.ChainDraft{draft}); err != nil {
		t.Fatalf("SaveCandidates() error = %v", err)
	}
	want := []int64{10, 20, 30}
	for i, clusterID := range want {
		if got := repository.saved[0].Participants[i].ClusterID; got != clusterID {
			t.Fatalf("participant %d cluster = %d, want %d", i, got, clusterID)
		}
	}
}

func TestSaveCandidatesRejectsRepeatedCluster(t *testing.T) {
	tests := []struct {
		name         string
		participants []entity.ChainDraftParticipant
	}{
		{
			name: "cluster",
			participants: []entity.ChainDraftParticipant{
				{ClusterID: 1, RequestID: 1},
				{ClusterID: 1, RequestID: 2},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{}
			service := chainservice.NewService(repository, fakeTransactionManager{})
			err := service.SaveCandidates(context.Background(), nil, []entity.ChainDraft{{
				Score:        0.8,
				Participants: test.participants,
			}})
			if !errors.Is(err, entity.ErrInvalidChainDraft) {
				t.Fatalf("SaveCandidates() error = %v", err)
			}
			if len(repository.saved) != 0 {
				t.Fatal("invalid draft must not be saved")
			}
		})
	}
}

func TestSaveCandidatesAllowsOneClusterInDifferentChains(t *testing.T) {
	repository := &fakeRepository{}
	service := chainservice.NewService(repository, fakeTransactionManager{})
	drafts := []entity.ChainDraft{
		{
			Score: 0.9,
			Participants: []entity.ChainDraftParticipant{
				{ClusterID: 1, RequestID: 1},
				{ClusterID: 2, RequestID: 2},
			},
		},
		{
			Score: 0.8,
			Participants: []entity.ChainDraftParticipant{
				{ClusterID: 1, RequestID: 1},
				{ClusterID: 3, RequestID: 3},
			},
		},
	}

	if err := service.SaveCandidates(context.Background(), nil, drafts); err != nil {
		t.Fatalf("SaveCandidates() error = %v", err)
	}
	if len(repository.saved) != 2 {
		t.Fatalf("saved drafts = %d, want 2", len(repository.saved))
	}
}

type fakeRepository struct {
	saved                   []entity.ChainDraft
	status                  entity.ChainStatus
	length                  int
	edges                   []entity.VoteEdge
	existingVote            entity.ChainVote
	proposed                []int64
	proposalDeadline        time.Time
	proposalExpired         bool
	upsertCalls             int
	deleteCalls             int
	markInProposal          int
	restoredActive          int
	validationErr           error
	approvedCount           int
	lockRequestCalls        int
	freezeCalled            bool
	lockAllInChain          int
	itemsUnavailable        int
	removedFromOthers       int
	requestIDs              []int64
	requestStatus           entity.ChainStatus
	edgeRequestID           int64
	edgeTargetID            int64
	edgeErr                 error
	requestLocks            int
	pendingCountCalls       int
	approvedCountCalls      int
	confirmedRequestID      int64
	confirmedTargetID       int64
	affectedChains          []int64
	releasedRequests        []int64
	thinkingCalls           int
	declineAvailable        bool
	declinedRequestID       int64
	fastReplacementEligible bool
	selectedRequestID       int64
	handoffStatus           entity.RequestStatus
	receiptStatus           entity.RequestStatus
	receiptErr              error
	handoffCalls            int
	startCalls              int
	doneCalls               int
	allDone                 bool
	completeCalls           int
}

func (r *fakeRepository) SaveCandidates(_ context.Context, _ database.Tx, drafts []entity.ChainDraft) error {
	r.saved = append(r.saved, drafts...)
	return nil
}

func (r *fakeRepository) List(_ context.Context, _ string) ([]entity.Chain, error) {
	return []entity.Chain{}, nil
}

func (r *fakeRepository) ListForOffer(_ context.Context, _ string, _ int64) ([]entity.Chain, error) {
	return []entity.Chain{}, nil
}

func (r *fakeRepository) Get(_ context.Context, _ string, _ int64) (entity.Chain, error) {
	return entity.Chain{}, nil
}

func (r *fakeRepository) LockForVote(_ context.Context, _ database.Tx, _ int64) (entity.ChainStatus, int, error) {
	status := r.status
	if status == "" {
		status = entity.ChainStatusCandidate
	}
	return status, r.length, nil
}

func (r *fakeRepository) ValidateVoteParticipants(_ context.Context, _ database.Tx, _ string, _, _, _ int64, _ int) error {
	return r.validationErr
}

func (r *fakeRepository) GetVote(_ context.Context, _ database.Tx, _ string, _, _, _ int64) (entity.ChainVote, error) {
	return r.existingVote, nil
}

func (r *fakeRepository) UpsertPendingVote(_ context.Context, _ database.Tx, _, _, _ int64) (time.Time, error) {
	r.upsertCalls++
	return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC), nil
}

func (r *fakeRepository) DeletePendingVote(_ context.Context, _ database.Tx, _, _, _ int64) error {
	r.deleteCalls++
	return nil
}

func (r *fakeRepository) ListPendingVoteEdges(_ context.Context, _ database.Tx, _ int64) ([]entity.VoteEdge, error) {
	return r.edges, nil
}

func (r *fakeRepository) Propose(_ context.Context, _ database.Tx, _ int64, requestIDs []int64, deadline time.Time) error {
	r.proposed = append([]int64(nil), requestIDs...)
	r.proposalDeadline = deadline
	return nil
}

func (r *fakeRepository) ExpireProposalIfDue(_ context.Context, _ database.Tx, _ int64) (bool, error) {
	return r.proposalExpired, nil
}

func (r *fakeRepository) MarkRequestInProposal(_ context.Context, _ database.Tx, _ int64) error {
	r.markInProposal++
	return nil
}

func (r *fakeRepository) RestoreActiveIfNoPendingVotes(_ context.Context, _ database.Tx, _ int64) error {
	r.restoredActive++
	return nil
}

func (r *fakeRepository) LoadScoreFeatures(_ context.Context, _ database.Tx, _ int64) ([]float64, []float64, []int, error) {
	return []float64{0.9, 0.9}, []float64{0.75, 0.75}, []int{1, 1}, nil
}

func (r *fakeRepository) CountPendingVoters(_ context.Context, _ database.Tx, _ int64) (int, error) {
	r.pendingCountCalls++
	return 0, nil
}

func (r *fakeRepository) UpdateScore(_ context.Context, _ database.Tx, _ int64, _ float64) error {
	return nil
}

// --- методы, добавленные для Scrum-32 (подтверждение/заморозка) ---

func (r *fakeRepository) ConfirmParticipant(_ context.Context, _ database.Tx, _, requestID, targetID int64) error {
	r.confirmedRequestID = requestID
	r.confirmedTargetID = targetID
	return nil
}

func (r *fakeRepository) MarkParticipantThinking(_ context.Context, _ database.Tx, _, _, _ int64) error {
	r.thinkingCalls++
	return nil
}

func (r *fakeRepository) DeclineParticipant(_ context.Context, _ database.Tx, _ int64, requestID int64, fastReplacementEligible bool) (bool, entity.ChainStatus, error) {
	r.declinedRequestID = requestID
	r.fastReplacementEligible = fastReplacementEligible
	if r.declineAvailable && fastReplacementEligible {
		return true, entity.ChainStatusProposed, nil
	}
	return false, entity.ChainStatusCandidate, nil
}

func (r *fakeRepository) ListReplacementOptions(_ context.Context, _ string, _ int64) ([]entity.ReplacementOption, error) {
	return nil, nil
}

func (r *fakeRepository) SelectReplacement(_ context.Context, _ database.Tx, _ string, _, requestID int64) error {
	r.selectedRequestID = requestID
	return nil
}

func (r *fakeRepository) CountApprovedVoters(_ context.Context, _ database.Tx, _ int64) (int, error) {
	r.approvedCountCalls++
	return r.approvedCount, nil
}

func (r *fakeRepository) CountApprovedVotersExcept(_ context.Context, _ database.Tx, _, _ int64) (int, error) {
	r.approvedCountCalls++
	return r.approvedCount, nil
}

func (r *fakeRepository) MarkRequestLocked(_ context.Context, _ database.Tx, _ int64) error {
	r.lockRequestCalls++
	return nil
}

func (r *fakeRepository) FreezeChain(_ context.Context, _ database.Tx, _ int64, _ time.Time) error {
	r.freezeCalled = true
	return nil
}

func (r *fakeRepository) LockRequestsInChain(_ context.Context, _ database.Tx, _ int64) error {
	r.lockAllInChain++
	return nil
}

func (r *fakeRepository) MarkItemsUnavailable(_ context.Context, _ database.Tx, _ int64) error {
	r.itemsUnavailable++
	return nil
}

func (r *fakeRepository) LoadChainRequestIDs(_ context.Context, _ database.Tx, _ int64) ([]int64, error) {
	return r.requestIDs, nil
}

func (r *fakeRepository) LockRequestsForFreeze(_ context.Context, _ database.Tx, _ []int64) error {
	r.requestLocks++
	return nil
}

func (r *fakeRepository) LoadRequestLiveChainStatus(_ context.Context, _ database.Tx, _ int64) (entity.ChainStatus, error) {
	return r.requestStatus, nil
}

func (r *fakeRepository) FindParticipantEdge(_ context.Context, _ database.Tx, _ int64, _ string) (int64, int64, error) {
	return r.edgeRequestID, r.edgeTargetID, r.edgeErr
}

func (r *fakeRepository) MarkRequestInProgress(_ context.Context, _ database.Tx, _, _ int64) (entity.RequestStatus, error) {
	r.handoffCalls++
	if r.handoffStatus == "" {
		return entity.RequestStatusInProgress, nil
	}
	return r.handoffStatus, nil
}

func (r *fakeRepository) StartChain(_ context.Context, _ database.Tx, _ int64) error {
	r.startCalls++
	return nil
}

func (r *fakeRepository) FindReceiptRequestStatus(_ context.Context, _ database.Tx, _, _ int64, _ string) (entity.RequestStatus, error) {
	if r.receiptErr != nil {
		return "", r.receiptErr
	}
	if r.receiptStatus == "" {
		return entity.RequestStatusInProgress, nil
	}
	return r.receiptStatus, nil
}

func (r *fakeRepository) MarkRequestDone(_ context.Context, _ database.Tx, _ int64) error {
	r.doneCalls++
	return nil
}

func (r *fakeRepository) AllChainRequestsDone(_ context.Context, _ database.Tx, _ int64) (bool, error) {
	return r.allDone, nil
}

func (r *fakeRepository) CompleteChain(_ context.Context, _ database.Tx, _ int64) error {
	r.completeCalls++
	return nil
}

type fakeTransactionManager struct{}

func (fakeTransactionManager) WithinTransaction(_ context.Context, fn func(database.Tx) error) error {
	return fn(nil)
}

func (r *fakeRepository) ListChainsContainingRequest(_ context.Context, _ database.Tx, _ int64) ([]int64, error) {
	return nil, nil
}
func (r *fakeRepository) DeleteRequestParticipation(_ context.Context, _ database.Tx, _ int64) error {
	return nil
}
func (r *fakeRepository) DeleteChain(_ context.Context, _ database.Tx, _ int64) error {
	return nil
}
func (r *fakeRepository) ReleaseCompetitorsFromOtherChains(_ context.Context, _ database.Tx, _ int64) ([]int64, error) {
	return r.affectedChains, nil
}

func (r *fakeRepository) ReleaseUnselectedFromChain(_ context.Context, _ database.Tx, _ int64) ([]int64, error) {
	return r.releasedRequests, nil
}

func TestThinkRecordsExplicitDecision(t *testing.T) {
	repository := &fakeRepository{status: entity.ChainStatusProposed, length: 3, edgeRequestID: 10, edgeTargetID: 20}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	if err := service.Think(context.Background(), "user-1", 7); err != nil {
		t.Fatalf("Think() error = %v", err)
	}
	if repository.thinkingCalls != 1 {
		t.Fatalf("thinking calls = %d, want 1", repository.thinkingCalls)
	}
}

func TestDeclineKeepsProposalWhenReplacementExists(t *testing.T) {
	repository := &fakeRepository{
		status: entity.ChainStatusProposed, length: 3,
		edgeRequestID: 10, edgeTargetID: 20, declineAvailable: true, approvedCount: 2,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	available, status, err := service.Decline(context.Background(), "user-1", 7)
	if err != nil {
		t.Fatalf("Decline() error = %v", err)
	}
	if !available || status != entity.ChainStatusProposed || repository.declinedRequestID != 10 || !repository.fastReplacementEligible {
		t.Fatalf("Decline() = available %v, status %s, request %d", available, status, repository.declinedRequestID)
	}
}

func TestDeclineRollsBackBeforeOtherParticipantsConfirm(t *testing.T) {
	repository := &fakeRepository{
		status: entity.ChainStatusProposed, length: 5, approvedCount: 1,
		edgeRequestID: 10, edgeTargetID: 20, declineAvailable: true,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	available, status, err := service.Decline(context.Background(), "user-1", 7)
	if err != nil {
		t.Fatalf("Decline() error = %v", err)
	}
	if available || status != entity.ChainStatusCandidate || repository.fastReplacementEligible {
		t.Fatalf("Decline() = available %v, status %s, fast replacement %v", available, status, repository.fastReplacementEligible)
	}
}

func TestDeclineDoesNotCountDecliningParticipantsApproval(t *testing.T) {
	repository := &fakeRepository{
		status: entity.ChainStatusProposed, length: 3,
		edgeRequestID: 10, edgeTargetID: 20,
		declineAvailable: true, approvedCount: 1,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	available, status, err := service.Decline(context.Background(), "user-1", 7)
	if err != nil {
		t.Fatalf("Decline() error = %v", err)
	}
	if available || status != entity.ChainStatusCandidate || repository.fastReplacementEligible {
		t.Fatalf("Decline() = available %v, status %s, fast replacement %v", available, status, repository.fastReplacementEligible)
	}
}

func TestDeclineRollsBackWhenReplacementMissing(t *testing.T) {
	repository := &fakeRepository{
		status: entity.ChainStatusProposed, length: 2,
		edgeRequestID: 10, edgeTargetID: 20,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	available, status, err := service.Decline(context.Background(), "user-1", 7)
	if err != nil {
		t.Fatalf("Decline() error = %v", err)
	}
	if available || status != entity.ChainStatusCandidate {
		t.Fatalf("Decline() = available %v, status %s", available, status)
	}
}

func TestSelectReplacementPinsRequestedCandidate(t *testing.T) {
	repository := &fakeRepository{status: entity.ChainStatusProposed, length: 3}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	if err := service.SelectReplacement(context.Background(), "user-1", 7, 99); err != nil {
		t.Fatalf("SelectReplacement() error = %v", err)
	}
	if repository.selectedRequestID != 99 {
		t.Fatalf("selected request = %d, want 99", repository.selectedRequestID)
	}
}

type fakeRebuilder struct {
	affected []int64
	rebuilt  []int64
}

func (r *fakeRebuilder) RepairAffectedChains(_ context.Context, _ database.Tx, affected []int64) error {
	r.affected = append([]int64(nil), affected...)
	return nil
}

func (r *fakeRebuilder) RebuildRequests(_ context.Context, _ database.Tx, requestIDs []int64) error {
	r.rebuilt = append([]int64(nil), requestIDs...)
	return nil
}

func TestConfirmKeepsProposedUntilEveryParticipantApproves(t *testing.T) {
	repository := &fakeRepository{
		status:        entity.ChainStatusProposed,
		length:        3,
		approvedCount: 1,
		requestIDs:    []int64{10, 20, 30},
		edgeRequestID: 10,
		edgeTargetID:  20,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{}).
		WithScorer(ranker.NewChainScoreCalculator(ranker.NewRankerConfig()))

	status, err := service.Confirm(context.Background(), "user-1", 7)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if status != entity.ChainStatusProposed {
		t.Fatalf("status = %s, want %s", status, entity.ChainStatusProposed)
	}
	if repository.lockRequestCalls != 1 || repository.requestLocks != 1 {
		t.Fatalf("lock calls: participant=%d, chain requests=%d", repository.lockRequestCalls, repository.requestLocks)
	}
	if repository.confirmedRequestID != 10 || repository.confirmedTargetID != 20 {
		t.Fatalf("confirmed edge = %d -> %d, want 10 -> 20", repository.confirmedRequestID, repository.confirmedTargetID)
	}
	if repository.pendingCountCalls != 0 || repository.approvedCountCalls != 2 {
		t.Fatalf("score voter calls: pending=%d, approved=%d", repository.pendingCountCalls, repository.approvedCountCalls)
	}
}

func TestConfirmFreezesWhenEveryParticipantApproved(t *testing.T) {
	repository := &fakeRepository{
		status:        entity.ChainStatusProposed,
		length:        2,
		approvedCount: 2,
		requestIDs:    []int64{10, 20},
		edgeRequestID: 10,
		edgeTargetID:  20,
	}
	rebuilder := &fakeRebuilder{}
	repository.affectedChains = []int64{8, 9}
	repository.releasedRequests = []int64{30, 40}
	freezer := chainservice.NewFreezeService(repository, rebuilder)
	service := chainservice.NewService(repository, fakeTransactionManager{}).WithFreezer(freezer)

	status, err := service.Confirm(context.Background(), "user-1", 7)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if status != entity.ChainStatusFrozen {
		t.Fatalf("status = %s, want %s", status, entity.ChainStatusFrozen)
	}
	if !repository.freezeCalled || repository.lockAllInChain != 1 || repository.itemsUnavailable != 1 {
		t.Fatalf("freeze calls: chain=%v requests=%d items=%d", repository.freezeCalled, repository.lockAllInChain, repository.itemsUnavailable)
	}
	if repository.requestLocks != 2 {
		t.Fatalf("request lock calls = %d, want 2 (confirm and freeze guard)", repository.requestLocks)
	}
	if len(rebuilder.affected) != 2 || rebuilder.affected[0] != 8 || rebuilder.affected[1] != 9 {
		t.Fatalf("rebuilt competitors = %v, want [8 9]", rebuilder.affected)
	}
	if len(rebuilder.rebuilt) != 2 || rebuilder.rebuilt[0] != 30 || rebuilder.rebuilt[1] != 40 {
		t.Fatalf("rebuilt released requests = %v, want [30 40]", rebuilder.rebuilt)
	}
}

func TestConfirmRejectsCandidateChain(t *testing.T) {
	repository := &fakeRepository{status: entity.ChainStatusCandidate, length: 2}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	_, err := service.Confirm(context.Background(), "user-1", 7)
	if !errors.Is(err, entity.ErrChainNotProposed) {
		t.Fatalf("Confirm() error = %v, want %v", err, entity.ErrChainNotProposed)
	}
	if repository.lockRequestCalls != 0 || repository.requestLocks != 0 {
		t.Fatal("candidate confirm must not lock requests")
	}
}

func TestConfirmExpiresOverdueProposal(t *testing.T) {
	repository := &fakeRepository{
		status: entity.ChainStatusProposed, length: 2,
		proposalExpired: true,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	_, err := service.Confirm(context.Background(), "user-1", 7)
	if !errors.Is(err, entity.ErrChainConfirmationExpired) {
		t.Fatalf("Confirm() error = %v, want %v", err, entity.ErrChainConfirmationExpired)
	}
	if repository.lockRequestCalls != 0 {
		t.Fatal("expired proposal must not accept a confirmation")
	}
}

func TestConfirmFrozenRetryRequiresParticipant(t *testing.T) {
	repository := &fakeRepository{
		status:  entity.ChainStatusFrozen,
		length:  2,
		edgeErr: entity.ErrChainVoteForbidden,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	_, err := service.Confirm(context.Background(), "stranger", 7)
	if !errors.Is(err, entity.ErrChainVoteForbidden) {
		t.Fatalf("Confirm() error = %v, want %v", err, entity.ErrChainVoteForbidden)
	}
}

func TestConfirmFrozenRetryIsIdempotentForParticipant(t *testing.T) {
	repository := &fakeRepository{
		status:        entity.ChainStatusFrozen,
		length:        2,
		edgeRequestID: 10,
		edgeTargetID:  20,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	status, err := service.Confirm(context.Background(), "user-1", 7)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if status != entity.ChainStatusFrozen {
		t.Fatalf("status = %s, want %s", status, entity.ChainStatusFrozen)
	}
	if repository.lockRequestCalls != 0 || repository.freezeCalled {
		t.Fatal("idempotent retry must not repeat locking or freezing")
	}
}

func TestHandoffStartsFrozenChainForPinnedRequest(t *testing.T) {
	repository := &fakeRepository{status: entity.ChainStatusFrozen}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	result, err := service.Handoff(context.Background(), 7, 10)
	if err != nil {
		t.Fatalf("Handoff() error = %v", err)
	}
	if result.Status != entity.ChainStatusInProgress {
		t.Fatalf("status = %s, want %s", result.Status, entity.ChainStatusInProgress)
	}
	if repository.handoffCalls != 1 || repository.startCalls != 1 {
		t.Fatalf("handoff calls = %d, start calls = %d", repository.handoffCalls, repository.startCalls)
	}
}

func TestConfirmReceiptRequiresHandoff(t *testing.T) {
	repository := &fakeRepository{
		status:        entity.ChainStatusInProgress,
		receiptStatus: entity.RequestStatusLocked,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	_, err := service.ConfirmReceipt(context.Background(), "recipient", 7, 10)
	if !errors.Is(err, entity.ErrChainHandoffPending) {
		t.Fatalf("ConfirmReceipt() error = %v, want %v", err, entity.ErrChainHandoffPending)
	}
	if repository.doneCalls != 0 {
		t.Fatal("receipt before handoff must not complete request")
	}
}

func TestConfirmReceiptCompletesChainAfterEveryRequestDone(t *testing.T) {
	repository := &fakeRepository{
		status:        entity.ChainStatusInProgress,
		receiptStatus: entity.RequestStatusInProgress,
		allDone:       true,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	result, err := service.ConfirmReceipt(context.Background(), "recipient", 7, 10)
	if err != nil {
		t.Fatalf("ConfirmReceipt() error = %v", err)
	}
	if result.Status != entity.ChainStatusCompleted {
		t.Fatalf("status = %s, want %s", result.Status, entity.ChainStatusCompleted)
	}
	if repository.doneCalls != 1 || repository.completeCalls != 1 {
		t.Fatalf("done calls = %d, complete calls = %d", repository.doneCalls, repository.completeCalls)
	}
}

func TestConfirmReceiptIsIdempotentAfterCompletion(t *testing.T) {
	repository := &fakeRepository{
		status:        entity.ChainStatusCompleted,
		receiptStatus: entity.RequestStatusDone,
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	result, err := service.ConfirmReceipt(context.Background(), "recipient", 7, 10)
	if err != nil {
		t.Fatalf("ConfirmReceipt() error = %v", err)
	}
	if result.Status != entity.ChainStatusCompleted || repository.completeCalls != 0 {
		t.Fatalf("result = %+v, complete calls = %d", result, repository.completeCalls)
	}
}

func TestFreezeRejectsRequestFromAnotherFrozenChain(t *testing.T) {
	repository := &fakeRepository{
		requestIDs:    []int64{10, 20},
		requestStatus: entity.ChainStatusFrozen,
	}
	freezer := chainservice.NewFreezeService(repository, nil)

	err := freezer.Freeze(context.Background(), nil, 7)
	if !errors.Is(err, entity.ErrRequestInTwoFrozenChains) {
		t.Fatalf("Freeze() error = %v, want %v", err, entity.ErrRequestInTwoFrozenChains)
	}
	if repository.freezeCalled {
		t.Fatal("chain must not freeze after detecting a competing frozen chain")
	}
}

// --- тесты раунда 1 (Vote / WithdrawVote) ---

func TestVoteProposesOnlyClosedPendingCycle(t *testing.T) {
	repository := &fakeRepository{
		length: 3,
		edges: []entity.VoteEdge{
			{RequestID: 10, TargetRequestID: 20, Position: 0},
			{RequestID: 10, TargetRequestID: 21, Position: 0},
			{RequestID: 20, TargetRequestID: 30, Position: 1},
			{RequestID: 21, TargetRequestID: 31, Position: 1},
			{RequestID: 30, TargetRequestID: 10, Position: 2},
		},
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	result, err := service.Vote(context.Background(), "user-1", 7, chainservice.VoteInput{
		RequestID: 30, TargetRequestID: 10,
	})
	if err != nil {
		t.Fatalf("Vote() error = %v", err)
	}
	if result.ChainStatus != entity.ChainStatusProposed {
		t.Fatalf("status = %s, want %s", result.ChainStatus, entity.ChainStatusProposed)
	}
	want := []int64{10, 20, 30}
	for i := range want {
		if repository.proposed[i] != want[i] {
			t.Fatalf("proposed cycle = %v, want %v", repository.proposed, want)
		}
	}
	if repository.proposalDeadline.Before(time.Now()) {
		t.Fatal("proposed chain must have a future confirmation deadline")
	}
}

func TestVoteKeepsCandidateWithoutClosedCycle(t *testing.T) {
	repository := &fakeRepository{
		length: 3,
		edges: []entity.VoteEdge{
			{RequestID: 10, TargetRequestID: 20, Position: 0},
			{RequestID: 20, TargetRequestID: 30, Position: 1},
		},
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	result, err := service.Vote(context.Background(), "user-1", 7, chainservice.VoteInput{
		RequestID: 20, TargetRequestID: 30,
	})
	if err != nil {
		t.Fatalf("Vote() error = %v", err)
	}
	if result.ChainStatus != entity.ChainStatusCandidate || len(repository.proposed) != 0 {
		t.Fatalf("status = %s, proposed = %v", result.ChainStatus, repository.proposed)
	}
	if repository.markInProposal != 1 {
		t.Fatalf("MarkRequestInProposal calls = %d, want 1", repository.markInProposal)
	}
}

func TestVoteRetryAfterProposalIsIdempotent(t *testing.T) {
	votedAt := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		status: entity.ChainStatusProposed,
		length: 3,
		existingVote: entity.ChainVote{
			ChainID: 7, RequestID: 10, TargetRequestID: 20,
			Vote: entity.VotePending, VotedAt: votedAt,
		},
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	result, err := service.Vote(context.Background(), "user-1", 7, chainservice.VoteInput{
		RequestID: 10, TargetRequestID: 20,
	})
	if err != nil {
		t.Fatalf("Vote() error = %v", err)
	}
	if result.ChainStatus != entity.ChainStatusProposed || repository.upsertCalls != 0 {
		t.Fatalf("result = %+v, upsert calls = %d", result, repository.upsertCalls)
	}
}

func TestWithdrawVoteIsIdempotentWhileCandidate(t *testing.T) {
	repository := &fakeRepository{length: 3}
	service := chainservice.NewService(repository, fakeTransactionManager{})
	input := chainservice.VoteInput{RequestID: 10, TargetRequestID: 20}

	for attempt := 0; attempt < 2; attempt++ {
		if err := service.WithdrawVote(context.Background(), "user-1", 7, input); err != nil {
			t.Fatalf("WithdrawVote() attempt %d error = %v", attempt+1, err)
		}
	}
	if repository.deleteCalls != 2 {
		t.Fatalf("delete calls = %d, want 2 idempotent DELETE executions", repository.deleteCalls)
	}
}

func TestWithdrawVoteRejectedAfterProposal(t *testing.T) {
	repository := &fakeRepository{status: entity.ChainStatusProposed, length: 3}
	service := chainservice.NewService(repository, fakeTransactionManager{})
	err := service.WithdrawVote(context.Background(), "user-1", 7, chainservice.VoteInput{
		RequestID: 10, TargetRequestID: 20,
	})
	if !errors.Is(err, entity.ErrChainNotCandidate) {
		t.Fatalf("WithdrawVote() error = %v, want %v", err, entity.ErrChainNotCandidate)
	}
	if repository.deleteCalls != 0 {
		t.Fatalf("delete calls = %d, want 0", repository.deleteCalls)
	}
}

func TestVoteProposesFourMemberChainWhenAllRespond(t *testing.T) {
	repository := &fakeRepository{
		length: 4,
		edges: []entity.VoteEdge{
			{RequestID: 10, TargetRequestID: 20, Position: 0},
			{RequestID: 20, TargetRequestID: 30, Position: 1},
			{RequestID: 30, TargetRequestID: 40, Position: 2},
			{RequestID: 40, TargetRequestID: 10, Position: 3},
		},
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	result, err := service.Vote(context.Background(), "user-1", 7, chainservice.VoteInput{
		RequestID: 40, TargetRequestID: 10,
	})
	if err != nil {
		t.Fatalf("Vote() error = %v", err)
	}
	if result.ChainStatus != entity.ChainStatusProposed {
		t.Fatalf("status = %s, want %s", result.ChainStatus, entity.ChainStatusProposed)
	}
	want := []int64{10, 20, 30, 40}
	for i := range want {
		if repository.proposed[i] != want[i] {
			t.Fatalf("proposed cycle = %v, want %v", repository.proposed, want)
		}
	}
	if repository.markInProposal != 1 {
		t.Fatalf("MarkRequestInProposal calls = %d, want 1", repository.markInProposal)
	}
}

func TestWithdrawVoteKeepsCandidateWhenCycleNotClosed(t *testing.T) {
	repository := &fakeRepository{
		length: 3,
		edges: []entity.VoteEdge{
			{RequestID: 10, TargetRequestID: 20, Position: 0},
			{RequestID: 20, TargetRequestID: 30, Position: 1},
		},
	}
	service := chainservice.NewService(repository, fakeTransactionManager{})

	err := service.WithdrawVote(context.Background(), "user-1", 7, chainservice.VoteInput{
		RequestID: 10, TargetRequestID: 20,
	})
	if err != nil {
		t.Fatalf("WithdrawVote() error = %v", err)
	}
	if repository.deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", repository.deleteCalls)
	}
	if repository.restoredActive != 1 {
		t.Fatalf("RestoreActiveIfNoPendingVotes calls = %d, want 1", repository.restoredActive)
	}
}
