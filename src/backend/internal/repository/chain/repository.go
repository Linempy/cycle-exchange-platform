package chain

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool              *pgxpool.Pool
	matchingThreshold float64
}

func NewRepository(pool *pgxpool.Pool, thresholds ...float64) *Postgres {
	threshold := 0.5
	if len(thresholds) > 0 && thresholds[0] > 0 && thresholds[0] <= 1 {
		threshold = thresholds[0]
	}
	return &Postgres{pool: pool, matchingThreshold: threshold}
}
