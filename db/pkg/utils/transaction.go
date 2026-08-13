package utils

import (
	"context"
	"database/sql"
	"errors"

	"github.com/alex99y/matching-engine/common/pkg/logger"
)

// BeginTx opens a transaction and returns a rollback func that is a safe no-op after a
// successful Commit — callers should `defer rollback()` right after checking err.
// logPrefix identifies the calling operation in log lines (e.g. "ProcessBatch",
// "FreezeUserBalance"). The returned error is unwrapped; callers wrap it with their
// own sentinel.
func BeginTx(
	ctx context.Context,
	db *sql.DB,
	log *logger.Logger,
	logPrefix string,
) (*sql.Tx, func(), error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Error(logPrefix + ": begin tx: " + err.Error())
		return nil, nil, err
	}

	rollback := func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			log.Error(logPrefix + ": rollback failed: " + rbErr.Error())
		}
	}
	return tx, rollback, nil
}
