package chain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Avito-Team-Not-Found/tricky-exchanger/internal/entity"
	chainservice "github.com/Avito-Team-Not-Found/tricky-exchanger/internal/service/chain"
)

func TestLifecycleMethodsRequireRepository(t *testing.T) {
	service := chainservice.NewService(nil, fakeTransactionManager{})
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"ListChainsContainingRequest", func() error {
			_, err := service.ListChainsContainingRequest(ctx, nil, 1)
			return err
		}},
		{"DeleteRequestParticipation", func() error {
			return service.DeleteRequestParticipation(ctx, nil, 1)
		}},
		{"DeleteChain", func() error {
			return service.DeleteChain(ctx, nil, 1)
		}},
		{"LoadChainRequestIDs", func() error {
			_, err := service.LoadChainRequestIDs(ctx, nil, 1)
			return err
		}},
		{"LoadActiveChainRequestIDs", func() error {
			_, err := service.LoadActiveChainRequestIDs(ctx, nil, 1)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, entity.ErrChainRepositoryNotConfigured) {
				t.Fatalf("error = %v, want %v", err, entity.ErrChainRepositoryNotConfigured)
			}
		})
	}
}

func TestFreezeRequiresRepository(t *testing.T) {
	freezer := chainservice.NewFreezeService(nil, nil)
	if err := freezer.Freeze(context.Background(), nil, 1); !errors.Is(err, entity.ErrChainRepositoryNotConfigured) {
		t.Fatalf("Freeze() error = %v", err)
	}
	if err := freezer.ExpireDue(context.Background(), nil); !errors.Is(err, entity.ErrChainRepositoryNotConfigured) {
		t.Fatalf("ExpireDue() error = %v", err)
	}
}
