package chain_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	chainhandler "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/handler/chain"
)

func TestListReturnsChainsForAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &fakeService{chain: entity.Chain{ID: 5, Status: entity.ChainStatusCandidate, Length: 2}}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.GET("/chains", chainhandler.NewHandler(service).List)

	req := httptest.NewRequest(http.MethodGet, "/chains", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":5`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestDeclineReturnsReplacementFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	service := &fakeService{declineAvailable: true, declineStatus: entity.ChainStatusProposed}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.POST("/chains/:id/decline", chainhandler.NewHandler(service).Decline)

	req := httptest.NewRequest(http.MethodPost, "/chains/9/decline", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"replacementAvailable":true`) || !strings.Contains(body, `"status":"PROPOSED"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestListReplacementsReturnsOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	service := &fakeService{replacements: []entity.ReplacementOption{{
		RequestID: 99, OfferedItemID: 1, Title: "alt", Reliability: 0.8,
	}}}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.GET("/chains/:id/replacements", chainhandler.NewHandler(service).ListReplacements)

	req := httptest.NewRequest(http.MethodGet, "/chains/3/replacements", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"requestId":99`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestSelectReplacementAcceptsBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	service := &fakeService{}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	engine.PUT("/chains/:id/replacement", chainhandler.NewHandler(service).SelectReplacement)

	req := httptest.NewRequest(http.MethodPut, "/chains/3/replacement", strings.NewReader(`{"requestId":42}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if service.selectedReplacementID != 42 || service.selectChainID != 3 {
		t.Fatalf("selected = chain %d request %d", service.selectChainID, service.selectedReplacementID)
	}
	if !strings.Contains(rec.Body.String(), `"requestId":42`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
