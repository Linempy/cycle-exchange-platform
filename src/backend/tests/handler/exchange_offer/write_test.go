package exchange_offer_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	offerhandler "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/handler/exchange_offer"
	offerservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/exchange_offer"
)

func TestCreateReturnsCreatedOffer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &writeFakeService{created: entity.ExchangeOffer{ID: 21, OfferedItemID: 1, WantedCategory: "phones"}}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", uuid.New())
		c.Next()
	})
	engine.POST("/exchange-offers", offerhandler.NewHandler(service).Create)

	req := httptest.NewRequest(
		http.MethodPost,
		"/exchange-offers",
		strings.NewReader(`{"offeredItemId":1,"wantedDescription":"iPhone","wantedCategory":"phones"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":21`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestDeleteReturnsNoContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &writeFakeService{}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", uuid.New())
		c.Next()
	})
	engine.DELETE("/exchange-offers/:id", offerhandler.NewHandler(service).Delete)

	req := httptest.NewRequest(http.MethodDelete, "/exchange-offers/8?version=2", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if service.deletedID != 8 || service.deletedVersion != 2 {
		t.Fatalf("deleted = id %d version %d", service.deletedID, service.deletedVersion)
	}
}

func TestUpdateMapsVersionConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &writeFakeService{updateErr: entity.ErrExchangeOfferVersionConflict}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", uuid.New())
		c.Next()
	})
	engine.PATCH("/exchange-offers/:id", offerhandler.NewHandler(service).Update)

	req := httptest.NewRequest(
		http.MethodPatch,
		"/exchange-offers/8",
		strings.NewReader(`{"offeredItemId":1,"wantedDescription":"x","wantedCategory":"phones","version":1}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

type writeFakeService struct {
	created        entity.ExchangeOffer
	updateErr      error
	deletedID      int64
	deletedVersion int64
}

func (s *writeFakeService) Create(context.Context, string, offerservice.CreateInput) (entity.ExchangeOffer, error) {
	return s.created, nil
}

func (s *writeFakeService) Get(context.Context, string, int64) (entity.ExchangeOffer, error) {
	return entity.ExchangeOffer{}, nil
}

func (s *writeFakeService) List(context.Context, string) ([]entity.ExchangeOfferListItem, error) {
	return nil, nil
}

func (s *writeFakeService) Update(context.Context, string, int64, offerservice.UpdateInput) (entity.ExchangeOffer, error) {
	return entity.ExchangeOffer{}, s.updateErr
}

func (s *writeFakeService) Delete(_ context.Context, _ string, id, version int64) error {
	s.deletedID = id
	s.deletedVersion = version
	return nil
}
