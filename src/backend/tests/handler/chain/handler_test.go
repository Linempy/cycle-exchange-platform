package chain_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	chainhandler "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/handler/chain"
	chainservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/chain"
)

func TestGetReturnsOrderedExchangePositions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	createdAt := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	pending := entity.VotePending
	approved := entity.VoteApproved
	thinking := entity.VoteThinking
	service := &fakeService{chain: entity.Chain{
		ID:               7,
		Status:           entity.ChainStatusCandidate,
		Score:            0.91,
		Length:           3,
		Version:          0,
		CurrentRequestID: 12,
		CurrentPosition:  0,
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
		Participants: []entity.ChainParticipant{
			{ClusterID: 1, RequestID: 12, Position: 0, OwnerUserID: userID.String(), OfferedItemID: 101, OfferedItemTitle: "Кружка", WantedDescription: "Кофемашина", Vote: &pending},
			{ClusterID: 2, RequestID: 13, Position: 1, OwnerUserID: uuid.NewString(), OfferedItemID: 102, OfferedItemTitle: "Кофемашина", WantedDescription: "Велосипед", Vote: &approved},
			{ClusterID: 2, RequestID: 15, Position: 1, OwnerUserID: uuid.NewString(), OfferedItemID: 104, OfferedItemTitle: "Компактная кофемашина", WantedDescription: "Велосипед"},
			{ClusterID: 3, RequestID: 14, Position: 2, OwnerUserID: uuid.NewString(), OfferedItemID: 103, OfferedItemTitle: "Велосипед", WantedDescription: "Кружка", Vote: &thinking},
		},
	}}

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.GET("/chains/:id", chainhandler.NewHandler(service).Get)

	request := httptest.NewRequest(http.MethodGet, "/chains/7", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	const want = `"currentPosition":0,"givesToPosition":2,"receivesFromPosition":1`
	if body := recorder.Body.String(); !strings.Contains(body, want) {
		t.Fatalf("body = %s, want fragment %s", body, want)
	}
	for _, requestID := range []string{`"requestId":13`, `"requestId":15`} {
		if body := recorder.Body.String(); !strings.Contains(body, requestID) {
			t.Fatalf("body = %s, want both requests of receiving cluster", body)
		}
	}
	for _, vote := range []string{`"vote":"pending"`, `"vote":"approved"`, `"vote":"thinking"`} {
		if body := recorder.Body.String(); !strings.Contains(body, vote) {
			t.Fatalf("body = %s, want participant vote %s", body, vote)
		}
	}
}

func TestGetReturnsNotFoundForUnavailableChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &fakeService{err: entity.ErrChainNotFound}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.GET("/chains/:id", chainhandler.NewHandler(service).Get)

	request := httptest.NewRequest(http.MethodGet, "/chains/99", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestGetReturnsGoneForExpiredConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &fakeService{err: entity.ErrChainConfirmationExpired}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.GET("/chains/:id", chainhandler.NewHandler(service).Get)

	request := httptest.NewRequest(http.MethodGet, "/chains/7", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusGone {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestExchangeOptionsReturnsOnlyNextClusterMembers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	pending := entity.VotePending
	service := &fakeService{chains: []entity.Chain{{
		ID:               7,
		Status:           entity.ChainStatusCandidate,
		Score:            0.93,
		Length:           4,
		CurrentRequestID: 2,
		CurrentPosition:  1,
		Participants: []entity.ChainParticipant{
			{ClusterID: 1, RequestID: 1, Position: 0, OfferedItemID: 101, OfferedItemTitle: "Ручка"},
			{ClusterID: 2, RequestID: 2, Position: 1, OwnerUserID: userID.String(), OfferedItemID: 102, OfferedItemTitle: "Маркер"},
			{ClusterID: 3, RequestID: 3, Position: 2, OfferedItemID: 103, OfferedItemTitle: "Кружка 1", Vote: &pending},
			{ClusterID: 3, RequestID: 4, Position: 2, OfferedItemID: 104, OfferedItemTitle: "Кружка 2"},
			{ClusterID: 4, RequestID: 5, Position: 3, OfferedItemID: 105, OfferedItemTitle: "Машинка"},
		},
	}}}

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.GET("/exchange-offers/:id/exchange-options", chainhandler.NewHandler(service).ExchangeOptions)

	request := httptest.NewRequest(http.MethodGet, "/exchange-offers/2/exchange-options", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, fragment := range []string{
		`"currentRequestId":2`,
		`"receivesFromPosition":2`,
		`"requestId":3`,
		`"requestId":4`,
		`"vote":"pending"`,
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("body = %s, want fragment %s", body, fragment)
		}
	}
	if strings.Contains(body, `"requestId":5`) {
		t.Fatalf("body = %s, receive options must not contain another position", body)
	}
}

type fakeService struct {
	chain                 entity.Chain
	chains                []entity.Chain
	vote                  entity.ChainVote
	voteInput             chainservice.VoteInput
	withdrawInput         chainservice.VoteInput
	confirmStatus         entity.ChainStatus
	confirmUserID         string
	confirmChainID        int64
	handoffResult         chainservice.FulfillmentResult
	receiptResult         chainservice.FulfillmentResult
	handoffChainID        int64
	handoffRequestID      int64
	receiptUserID         string
	receiptChainID        int64
	receiptRequestID      int64
	declineAvailable      bool
	declineStatus         entity.ChainStatus
	replacements          []entity.ReplacementOption
	selectChainID         int64
	selectedReplacementID int64
	err                   error
}

func (s *fakeService) Confirm(_ context.Context, userID string, chainID int64) (entity.ChainStatus, error) {
	s.confirmUserID = userID
	s.confirmChainID = chainID
	return s.confirmStatus, s.err
}

func (s *fakeService) Unconfirm(_ context.Context, _ string, _ int64) (entity.ChainStatus, error) {
	return entity.ChainStatusProposed, s.err
}

func (s *fakeService) Think(_ context.Context, _ string, _ int64) error { return s.err }

func (s *fakeService) Decline(_ context.Context, _ string, _ int64) (bool, entity.ChainStatus, error) {
	status := s.declineStatus
	if status == "" {
		status = entity.ChainStatusCandidate
	}
	return s.declineAvailable, status, s.err
}

func (s *fakeService) ListReplacements(_ context.Context, _ string, _ int64) ([]entity.ReplacementOption, error) {
	return s.replacements, s.err
}

func (s *fakeService) SelectReplacement(_ context.Context, _ string, chainID, requestID int64) error {
	s.selectChainID = chainID
	s.selectedReplacementID = requestID
	return s.err
}

func (s *fakeService) Handoff(_ context.Context, chainID, requestID int64) (chainservice.FulfillmentResult, error) {
	s.handoffChainID = chainID
	s.handoffRequestID = requestID
	return s.handoffResult, s.err
}

func (s *fakeService) ConfirmReceipt(_ context.Context, userID string, chainID, requestID int64) (chainservice.FulfillmentResult, error) {
	s.receiptUserID = userID
	s.receiptChainID = chainID
	s.receiptRequestID = requestID
	return s.receiptResult, s.err
}

func (s *fakeService) List(_ context.Context, _ string) ([]entity.Chain, error) {
	return []entity.Chain{s.chain}, s.err
}

func (s *fakeService) ListForOffer(_ context.Context, _ string, _ int64) ([]entity.Chain, error) {
	return s.chains, s.err
}

func (s *fakeService) Get(_ context.Context, _ string, _ int64) (entity.Chain, error) {
	return s.chain, s.err
}

func (s *fakeService) Vote(_ context.Context, _ string, _ int64, input chainservice.VoteInput) (entity.ChainVote, error) {
	s.voteInput = input
	return s.vote, s.err
}

func (s *fakeService) WithdrawVote(_ context.Context, _ string, _ int64, input chainservice.VoteInput) error {
	s.withdrawInput = input
	return s.err
}

func TestVoteAcceptsConcreteExchangeOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &fakeService{vote: entity.ChainVote{
		ChainID: 7, RequestID: 10, TargetRequestID: 20,
		Vote:        entity.VotePending,
		VotedAt:     time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
		ChainStatus: entity.ChainStatusCandidate,
	}}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.PUT("/chains/:id/votes", chainhandler.NewHandler(service).Vote)

	request := httptest.NewRequest(
		http.MethodPut,
		"/chains/7/votes",
		strings.NewReader(`{"requestId":10,"targetRequestId":20}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.voteInput.RequestID != 10 || service.voteInput.TargetRequestID != 20 {
		t.Fatalf("input = %+v", service.voteInput)
	}
	for _, fragment := range []string{`"vote":"pending"`, `"chainStatus":"CANDIDATE"`} {
		if !strings.Contains(recorder.Body.String(), fragment) {
			t.Fatalf("body = %s, want %s", recorder.Body.String(), fragment)
		}
	}
}

func TestWithdrawVoteUsesSourceAndTargetQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &fakeService{}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.DELETE("/chains/:id/votes", chainhandler.NewHandler(service).WithdrawVote)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/chains/7/votes?requestId=10&targetRequestId=20",
		nil,
	)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.withdrawInput.RequestID != 10 || service.withdrawInput.TargetRequestID != 20 {
		t.Fatalf("input = %+v", service.withdrawInput)
	}
}

func TestConfirmReturnsFrozenStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &fakeService{confirmStatus: entity.ChainStatusFrozen}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.POST("/chains/:id/confirm", chainhandler.NewHandler(service).Confirm)

	request := httptest.NewRequest(http.MethodPost, "/chains/7/confirm", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.confirmUserID != userID.String() || service.confirmChainID != 7 {
		t.Fatalf("confirm call = user %q, chain %d", service.confirmUserID, service.confirmChainID)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"FROZEN"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestConfirmMapsForeignParticipantToForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &fakeService{err: entity.ErrChainVoteForbidden}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.POST("/chains/:id/confirm", chainhandler.NewHandler(service).Confirm)

	request := httptest.NewRequest(http.MethodPost, "/chains/7/confirm", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandoffAcceptsPinnedRequestWithoutJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeService{handoffResult: chainservice.FulfillmentResult{
		ChainID: 7, RequestID: 10, Status: entity.ChainStatusInProgress,
	}}
	engine := gin.New()
	engine.POST("/integrations/avito/handoffs", chainhandler.NewHandler(service).Handoff)

	request := httptest.NewRequest(
		http.MethodPost,
		"/integrations/avito/handoffs",
		strings.NewReader(`{"chainId":7,"requestId":10}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.handoffChainID != 7 || service.handoffRequestID != 10 {
		t.Fatalf("handoff = chain %d, request %d", service.handoffChainID, service.handoffRequestID)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"IN_PROGRESS"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestReceiptRequiresAuthenticatedRecipient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &fakeService{receiptResult: chainservice.FulfillmentResult{
		ChainID: 7, RequestID: 10, Status: entity.ChainStatusCompleted,
	}}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.POST("/chains/:id/receipt", chainhandler.NewHandler(service).ConfirmReceipt)

	request := httptest.NewRequest(
		http.MethodPost,
		"/chains/7/receipt",
		strings.NewReader(`{"requestId":10}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.receiptUserID != userID.String() || service.receiptChainID != 7 || service.receiptRequestID != 10 {
		t.Fatalf("receipt = user %q, chain %d, request %d", service.receiptUserID, service.receiptChainID, service.receiptRequestID)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"COMPLETED"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}
