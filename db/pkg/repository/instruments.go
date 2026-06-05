package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/postgres"
)

const instrumentErrPrefix = "instrument repository:"

var (
	ErrInstrumentAlreadyExists     = errors.New("instrument already exists")
	ErrInstrumentInsertFailed      = errors.New("failed to insert instrument")
	ErrInstrumentNotInserted       = errors.New("instrument not inserted")
	ErrInstrumentNotFound          = errors.New("instrument not found")
	ErrInstrumentGetFailed         = errors.New("failed to get instrument")
	ErrInstrumentDeleteFailed      = errors.New("failed to delete instrument")
	ErrInstrumentNotDeleted        = errors.New("instrument not deleted")
	ErrUnknownInstrumentConstraint = errors.New("unknown instrument constraint violation")
)

// instruments_symbol_key is the PostgreSQL default unique constraint name for `symbol UNIQUE`.
const InstrumentSymbolUniqueConstraint = "instruments_symbol_key"

type Instrument struct {
	ID        int
	Name      string
	Symbol    string
	Decimals  int
	CreatedAt time.Time
}

type InstrumentRepository struct {
	psql   *sql.DB
	logger *logger.Logger
}

func (r *InstrumentRepository) CreateNewInstrument(ctx context.Context, name, symbol string, decimals int) error {
	query := `
		INSERT INTO instruments (name, symbol, decimals)
		VALUES ($1, $2, $3)
	`
	row, err := r.psql.ExecContext(ctx, query, name, symbol, decimals)
	if err != nil {
		if constraint, isUnique := postgres.IsUniqueConstraintViolation(err); isUnique {
			if constraint == InstrumentSymbolUniqueConstraint {
				return fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentAlreadyExists)
			}
			r.logger.Error("unknown unique constraint violation when inserting instrument")
			r.logger.ErrorO(err)
			return fmt.Errorf("%s %w", instrumentErrPrefix, ErrUnknownInstrumentConstraint)
		}
		r.logger.Error("error inserting instrument")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentInsertFailed)
	}

	rowsAffected, err := row.RowsAffected()
	if rowsAffected == 0 || err != nil {
		r.logger.Error("no rows affected when inserting instrument")
		if err != nil {
			r.logger.ErrorO(err)
		}
		return fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentNotInserted)
	}
	return nil
}

func (r *InstrumentRepository) GetInstrument(ctx context.Context, symbol string) (*Instrument, error) {
	query := `
		SELECT id, name, symbol, decimals, created_at
		FROM instruments
		WHERE symbol = $1
	`
	row := r.psql.QueryRowContext(ctx, query, symbol)
	instrument := &Instrument{}

	err := row.Scan(&instrument.ID, &instrument.Name, &instrument.Symbol, &instrument.Decimals, &instrument.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentNotFound)
		}
		r.logger.Error("error scanning instrument")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentGetFailed)
	}

	return instrument, nil
}

func (r *InstrumentRepository) GetInstruments(ctx context.Context) ([]Instrument, error) {
	query := `
		SELECT id, name, symbol, decimals, created_at
		FROM instruments
		ORDER BY name ASC
	`
	rows, err := r.psql.QueryContext(ctx, query)
	if err != nil {
		r.logger.Error("error querying instruments")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentGetFailed)
	}
	defer rows.Close()

	instruments := []Instrument{}
	for rows.Next() {
		var i Instrument
		if err := rows.Scan(&i.ID, &i.Name, &i.Symbol, &i.Decimals, &i.CreatedAt); err != nil {
			r.logger.Error("error scanning instrument row")
			r.logger.ErrorO(err)
			return nil, fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentGetFailed)
		}
		instruments = append(instruments, i)
	}
	if err := rows.Err(); err != nil {
		r.logger.Error("error iterating instrument rows")
		r.logger.ErrorO(err)
		return nil, fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentGetFailed)
	}

	return instruments, nil
}

func (r *InstrumentRepository) RemoveOneInstrument(ctx context.Context, symbol string) error {
	query := `DELETE FROM instruments WHERE symbol = $1`
	result, err := r.psql.ExecContext(ctx, query, symbol)
	if err != nil {
		r.logger.Error("error deleting instrument")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentDeleteFailed)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("error getting rows affected for instrument delete")
		r.logger.ErrorO(err)
		return fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentDeleteFailed)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%s %w", instrumentErrPrefix, ErrInstrumentNotFound)
	}
	return nil
}

func NewInstrumentRepository(logger *logger.Logger, psql *sql.DB) *InstrumentRepository {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if psql == nil {
		panic("psql cannot be nil")
	}
	return &InstrumentRepository{
		psql:   psql,
		logger: logger,
	}
}
