package chain_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	chainhandler "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/handler/chain"
)

func TestDetermineErrorStatusMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"not_found", entity.ErrChainNotFound, http.StatusNotFound},
		{"not_proposed", entity.ErrChainNotProposed, http.StatusConflict},
		{"handoff_pending", entity.ErrChainHandoffPending, http.StatusConflict},
		{"vote_forbidden", entity.ErrChainVoteForbidden, http.StatusForbidden},
		{"receipt_forbidden", entity.ErrChainReceiptForbidden, http.StatusForbidden},
		{"handoff_invalid", entity.ErrHandoffRequestInvalid, http.StatusUnprocessableEntity},
		{"internal", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			chainhandler.DetermineError(c, tc.err)
			if recorder.Code != tc.code {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tc.code, recorder.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("json: %v", err)
			}
		})
	}
}
