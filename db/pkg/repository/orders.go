package repository

import (
	"database/sql"

	"github.com/alex99y/matching-engine/common/pkg/logger"
)

type OrderRepository struct {
	psql   *sql.DB
	logger *logger.Logger
}

func (o *OrderRepository) CreateNewOrder() (string, error) {
	return "", nil
}

func NewOrderRepository(logger *logger.Logger, psql *sql.DB) *OrderRepository {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if psql == nil {
		panic("psql cannot be nil")
	}
	return &OrderRepository{
		psql:   psql,
		logger: logger,
	}
}
