package cluster

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}
