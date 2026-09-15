package utils

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
)

var (
	errBeginFailed    = errors.New("connection refused")
	errRollbackFailed = errors.New("rollback exploded")
)

// A stub driver rather than a mocking library: BeginTx's whole contract is about what Rollback does
// after a Commit, and database/sql's own tx state machine is the thing under test alongside it.
type stubDriver struct{ conn *stubConn }

func (d *stubDriver) Open(string) (driver.Conn, error) { return d.conn, nil }

type stubConn struct {
	mu           sync.Mutex
	beginErr     error
	rollbackErr  error
	commits      int
	rollbacks    int
	rollbackErrs int
}

func (c *stubConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (c *stubConn) Close() error                        { return nil }

func (c *stubConn) Begin() (driver.Tx, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return &stubTx{conn: c}, nil
}

func (c *stubConn) counts() (commits, rollbacks, rollbackErrs int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.commits, c.rollbacks, c.rollbackErrs
}

type stubTx struct{ conn *stubConn }

func (t *stubTx) Commit() error {
	t.conn.mu.Lock()
	defer t.conn.mu.Unlock()
	t.conn.commits++
	return nil
}

func (t *stubTx) Rollback() error {
	t.conn.mu.Lock()
	defer t.conn.mu.Unlock()
	t.conn.rollbacks++
	if t.conn.rollbackErr != nil {
		t.conn.rollbackErrs++
		return t.conn.rollbackErr
	}
	return nil
}

func newStubDB(t *testing.T, conn *stubConn) *sql.DB {
	t.Helper()
	db := sql.OpenDB(stubConnector{conn: conn})
	t.Cleanup(func() { db.Close() })
	return db
}

type stubConnector struct{ conn *stubConn }

func (c stubConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c stubConnector) Driver() driver.Driver                        { return &stubDriver{conn: c.conn} }

func testLogger() *logger.Logger { return logger.NewLogger(logger.Error) }

// Callers `defer rollback()` immediately and then commit on the happy path, so the deferred rollback
// always runs against an already-committed transaction. It has to be silent there — database/sql
// answers ErrTxDone, and treating that as a failure would log an error on every successful batch.
func TestRollbackAfterCommitIsASilentNoop(t *testing.T) {
	conn := &stubConn{}
	db := newStubDB(t, conn)

	tx, rollback, err := BeginTx(context.Background(), db, testLogger(), "ProcessBatch")
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer rollback()

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	rollback()

	commits, _, rollbackErrs := conn.counts()
	if commits != 1 {
		t.Fatalf("commits = %d, want 1", commits)
	}
	// The driver is never asked to roll back a committed tx; database/sql short-circuits to ErrTxDone.
	if rollbackErrs != 0 {
		t.Fatalf("the driver reported %d rollback errors after a commit", rollbackErrs)
	}
}

// The failure path: no commit happened, so the deferred rollback is what actually undoes the work.
func TestRollbackWithoutACommitReachesTheDriver(t *testing.T) {
	conn := &stubConn{}
	db := newStubDB(t, conn)

	_, rollback, err := BeginTx(context.Background(), db, testLogger(), "ProcessBatch")
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	rollback()

	commits, rollbacks, _ := conn.counts()
	if commits != 0 {
		t.Fatalf("commits = %d, want 0", commits)
	}
	if rollbacks != 1 {
		t.Fatalf("rollbacks = %d, want 1", rollbacks)
	}
}

// rollback is deferred and may also be called explicitly on an error path, so a second call must not
// panic or report anything.
func TestRollbackIsIdempotent(t *testing.T) {
	conn := &stubConn{}
	db := newStubDB(t, conn)

	_, rollback, err := BeginTx(context.Background(), db, testLogger(), "ProcessBatch")
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	rollback()
	rollback()
	rollback()

	if _, rollbacks, _ := conn.counts(); rollbacks != 1 {
		t.Fatalf("the driver saw %d rollbacks, want 1", rollbacks)
	}
}

// A rollback that genuinely fails is a real problem — the transaction may still be holding locks —
// so it is logged rather than swallowed. It still must not panic: rollback returns nothing, and the
// caller has already deferred it.
func TestAGenuineRollbackFailureIsSurvivable(t *testing.T) {
	conn := &stubConn{rollbackErr: errRollbackFailed}
	db := newStubDB(t, conn)

	_, rollback, err := BeginTx(context.Background(), db, testLogger(), "ProcessBatch")
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	rollback()

	if _, _, rollbackErrs := conn.counts(); rollbackErrs != 1 {
		t.Fatalf("rollback errors = %d, want 1", rollbackErrs)
	}
}

// When the transaction never opened there is nothing to defer, so both returns are nil and the
// caller's `if err != nil { return }` has to come before its `defer rollback()`.
func TestBeginTxReturnsNoRollbackWhenItFails(t *testing.T) {
	conn := &stubConn{beginErr: errBeginFailed}
	db := newStubDB(t, conn)

	tx, rollback, err := BeginTx(context.Background(), db, testLogger(), "ProcessBatch")
	if err == nil {
		t.Fatal("expected an error")
	}
	if tx != nil {
		t.Fatal("a failed BeginTx returned a transaction")
	}
	if rollback != nil {
		t.Fatal("a failed BeginTx returned a rollback func")
	}
	// The error is returned unwrapped for the caller to wrap with its own sentinel.
	if !errors.Is(err, errBeginFailed) {
		t.Fatalf("err = %v, want the driver's error", err)
	}
}

// A cancelled context must not yield a transaction the caller would go on to use.
func TestBeginTxHonoursACancelledContext(t *testing.T) {
	db := newStubDB(t, &stubConn{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tx, rollback, err := BeginTx(ctx, db, testLogger(), "ProcessBatch")
	if err == nil {
		if rollback != nil {
			rollback()
		}
		t.Fatalf("a cancelled context produced a transaction: %v", tx)
	}
}
