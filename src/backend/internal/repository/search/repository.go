package search

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

type Search struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Search {
	return &Search{pool: pool}
}
