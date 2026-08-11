package exchange_offer_test

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
	offerhandler "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/handler/exchange_offer"
	offerservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/exchange_offer"
)

func TestListUsesAuthenticatedUserFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &handlerFakeService{
		list: []entity.ExchangeOfferListItem{{
			ExchangeOffer: entity.ExchangeOffer{
				ID:                12,
				OfferedItemID:     5,
				WantedDescription: "кофемашина",
				Status:            entity.RequestStatusActive,
				Version:           1,
				CreatedAt:         time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC),
				UpdatedAt:         time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC),
			},
			OfferedItemTitle: "Велосипед",
		}},
	}
	handler := offerhandler.NewHandler(service)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", uuid.MustParse("11111111-1111-1111-1111-111111111111"))
		c.Next()
	})
	routes := engine.Group("/exchange-offers")
	routes.GET("", handler.List)

	request := httptest.NewRequest(http.MethodGet, "/exchange-offers", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	const userID = "11111111-1111-1111-1111-111111111111"
	if service.listUserID != userID {
		t.Fatalf("list user ID = %q, want %s", service.listUserID, userID)
	}
	if got, want := recorder.Body.String(), `[{"id":12,"offeredItemId":5,"wantedDescription":"кофемашина","wantedCategory":"","status":"ACTIVE","version":1,"createdAt":"2026-08-07T09:00:00Z","updatedAt":"2026-08-07T09:00:00Z","offeredItemTitle":"Велосипед"}]`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestListRequiresAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := offerhandler.NewHandler(&handlerFakeService{})
	engine := gin.New()
	routes := engine.Group("/exchange-offers")
	routes.GET("", handler.List)

	request := httptest.NewRequest(http.MethodGet, "/exchange-offers", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestCreateRequiresWantedCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := offerhandler.NewHandler(&handlerFakeService{})
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("userID", uuid.New())
		c.Next()
	})
	engine.POST("/exchange-offers", handler.Create)

	request := httptest.NewRequest(
		http.MethodPost,
		"/exchange-offers",
		strings.NewReader(`{"offeredItemId":1,"wantedDescription":"iPhone"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
}

type handlerFakeService struct {
	list       []entity.ExchangeOfferListItem
	listUserID string
}

func (s *handlerFakeService) Create(context.Context, string, offerservice.CreateInput) (entity.ExchangeOffer, error) {
	return entity.ExchangeOffer{}, nil
}

func (s *handlerFakeService) Get(context.Context, string, int64) (entity.ExchangeOffer, error) {
	return entity.ExchangeOffer{}, nil
}

func (s *handlerFakeService) List(_ context.Context, userID string) ([]entity.ExchangeOfferListItem, error) {
	s.listUserID = userID
	return s.list, nil
}

func (s *handlerFakeService) Update(context.Context, string, int64, offerservice.UpdateInput) (entity.ExchangeOffer, error) {
	return entity.ExchangeOffer{}, nil
}

func (s *handlerFakeService) Delete(context.Context, string, int64, int64) error {
	return nil
}
