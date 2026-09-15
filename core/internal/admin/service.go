// Package admin is core's operator control plane: the per-market circuit breaker that halts and
// resumes trading. It is deliberately separate from the Prometheus listener — pausing a market is a
// privileged action, and the metrics port is scraped by monitoring that has no business halting a
// market.
package admin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// maxUserOrders bounds the listing and the "cancel every open order" sweep. An account with more
// resting orders than this needs more than one pass — deliberate, so a single admin call cannot
// publish an unbounded burst onto the command queues.
const maxUserOrders = 500

var (
	ErrMarketNotServed = errors.New("market not served by this core")
	ErrUserNotFound    = errors.New("user not found")
	ErrUserNotFrozen   = errors.New("user must be frozen before their orders can be cancelled")
	ErrNoTarget        = errors.New("specify order_ids or all")
	// errNotLive: the order is neither resting nor a pending exit, so there is nothing to cancel.
	errNotLive = errors.New("order is not live")
)

// Per-order reasons a cancel could not be published. They are outcomes, not failures of the request:
// the caller gets them alongside the orders that were cancelled.
const (
	reasonNotOpen         = "order is not open"
	reasonMarketNotServed = "market not served by this core"
	reasonMarketPaused    = "market is paused; resume it before cancelling"
	reasonPublishFailed   = "publish failed"
)

// MarketController is the subset of orderprocessors.OrderProcessor this service needs. Declared here
// (the consumer) per the layer-architecture rule; exported so main can build the map.
type MarketController interface {
	Pause()
	Resume()
	IsPaused() bool
	EmitCancel(ctx context.Context, orderID uuid.UUID) error
}

// userRepository / orderRepository / marketCache are the slices of the data layer this service
// needs, declared here per the layer-architecture rule.
type userRepository interface {
	GetUserByUsername(ctx context.Context, username string) (*repository.User, error)
}

type orderRepository interface {
	GetOrdersByUser(ctx context.Context, userID uuid.UUID, showOpenOrders, showCanceledOrders bool,
		baseInstrumentID, quoteInstrumentID *int, startDate, endDate *time.Time, limit int) ([]repository.OrderRow, error)
	GetOrdersByIDs(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]repository.OrderRow, error)
}

type marketCache interface {
	GetMarketByID(id int) (*repository.Market, error)
	GetMarkets() []repository.Market
}

type MarketStatus struct {
	Market string `json:"market"`
	Paused bool   `json:"paused"`
}

// Service halts and resumes the markets this core process serves. The set is fixed at startup from
// MARKET_LIST, so a ref that is absent is a genuine 404 rather than a market that merely has no
// orders — an operator halting the wrong core should be told, not silently ignored.
type Service struct {
	markets map[string]MarketController
	users   userRepository
	orders  orderRepository
	cache   marketCache
}

func NewService(markets map[string]MarketController, users userRepository, orders orderRepository, cache marketCache) *Service {
	if markets == nil {
		panic("markets cannot be nil")
	}
	return &Service{markets: markets, users: users, orders: orders, cache: cache}
}

// UserOrder is one of a user's orders as an operator sees it. Reachable reports whether this core
// could actually cancel it right now, so the reason is visible before the cancel is attempted rather
// than only in its result.
type UserOrder struct {
	OrderID       string  `json:"order_id"`
	ClientOrderID string  `json:"client_order_id,omitempty"`
	Market        string  `json:"market,omitempty"`
	Side          string  `json:"side,omitempty"`
	Price         *uint64 `json:"price,omitempty"`
	Remaining     *uint64 `json:"remaining,omitempty"`
	Type          string  `json:"type"`
	TimeInForce   string  `json:"time_in_force"`
	Status        string  `json:"status"`
	CreatedAt     int64   `json:"created_at"`
	Open          bool    `json:"open"`
	Reachable     bool    `json:"reachable"`
	Unreachable   string  `json:"unreachable_reason,omitempty"`
}

type UserOrders struct {
	Username string      `json:"username"`
	UserID   string      `json:"user_id"`
	Frozen   bool        `json:"frozen"`
	Orders   []UserOrder `json:"orders"`
}

// CancelResult is one order's outcome. Reason is empty when the cancel was published; a non-empty
// Reason means nothing was published for that order and it is still live.
type CancelResult struct {
	OrderID string `json:"order_id"`
	Market  string `json:"market,omitempty"`
	Queued  bool   `json:"queued"`
	Reason  string `json:"reason,omitempty"`
}

type CancelSummary struct {
	Username string         `json:"username"`
	Queued   int            `json:"queued"`
	Skipped  int            `json:"skipped"`
	Results  []CancelResult `json:"results"`
}

// ListUserOrders returns the user's orders. Open orders are what an operator acts on, so the
// default is open-only; cancelled ones are available for context.
func (s *Service) ListUserOrders(ctx context.Context, username string, includeOpen, includeCancelled bool) (*UserOrders, error) {
	user, err := s.lookupUser(ctx, username)
	if err != nil {
		return nil, err
	}

	rows, err := s.orders.GetOrdersByUser(ctx, user.ID, includeOpen, includeCancelled, nil, nil, nil, nil, maxUserOrders)
	if err != nil {
		return nil, fmt.Errorf("list user orders: %w", err)
	}

	out := &UserOrders{
		Username: user.Username,
		UserID:   user.ID.String(),
		Frozen:   user.Frozen,
		Orders:   make([]UserOrder, 0, len(rows)),
	}
	for i := range rows {
		out.Orders = append(out.Orders, s.describe(&rows[i]))
	}
	return out, nil
}

// CancelUserOrders publishes a cancel for each targeted order. It refuses outright unless the
// account is frozen; beyond that, an order it cannot reach is reported rather than failing the whole
// request, so one paused market does not block cleaning up the user's other markets.
//
// Publishing is not cancelling: the matcher applies each cancel in turn. Queued means the command is
// on its way, and re-listing the user's orders is how an operator confirms the outcome.
func (s *Service) CancelUserOrders(ctx context.Context, username string, orderIDs []uuid.UUID, all bool) (*CancelSummary, error) {
	if !all && len(orderIDs) == 0 {
		return nil, ErrNoTarget
	}

	user, err := s.lookupUser(ctx, username)
	if err != nil {
		return nil, err
	}
	if !user.Frozen {
		return nil, ErrUserNotFrozen
	}

	var rows []repository.OrderRow
	if all {
		rows, err = s.orders.GetOrdersByUser(ctx, user.ID, true, false, nil, nil, nil, nil, maxUserOrders)
	} else {
		// User-scoped by the query itself, so an id belonging to someone else simply is not found.
		rows, err = s.orders.GetOrdersByIDs(ctx, user.ID, orderIDs)
	}
	if err != nil {
		return nil, fmt.Errorf("cancel user orders: %w", err)
	}

	summary := &CancelSummary{Username: user.Username, Results: make([]CancelResult, 0, len(rows))}
	for i := range rows {
		result := s.cancelOne(ctx, &rows[i])
		if result.Queued {
			summary.Queued++
		} else {
			summary.Skipped++
		}
		summary.Results = append(summary.Results, result)
	}
	return summary, nil
}

// reach answers which market an order sits on and whether this core can act on it right now. A
// non-empty reason means it cannot, and market is nil.
//
// The listing and the cancel must agree on this: the listing's "reachable" is a promise that the
// cancel will not refuse for the same reason moments later, so they ask here rather than each
// deciding for themselves.
func (s *Service) reach(row *repository.OrderRow) (marketRef string, market MarketController, reason string) {
	marketRef, err := s.rowMarketRef(row)
	if err != nil {
		if errors.Is(err, errNotLive) {
			return "", nil, reasonNotOpen
		}
		return "", nil, reasonMarketNotServed
	}

	market, served := s.markets[marketRef]
	switch {
	case !served:
		return marketRef, nil, reasonMarketNotServed
	case market.IsPaused():
		// A paused market is not consuming, so a cancel would sit in its queue indefinitely. Say so
		// rather than reporting a success for an order that is still matchable.
		return marketRef, nil, reasonMarketPaused
	}
	return marketRef, market, ""
}

func (s *Service) cancelOne(ctx context.Context, row *repository.OrderRow) CancelResult {
	marketRef, market, reason := s.reach(row)
	result := CancelResult{OrderID: row.ID.String(), Market: marketRef, Reason: reason}
	if reason != "" {
		return result
	}

	if err := market.EmitCancel(ctx, row.ID); err != nil {
		result.Reason = reasonPublishFailed
		return result
	}

	result.Queued = true
	return result
}

func (s *Service) describe(row *repository.OrderRow) UserOrder {
	marketRef, market, reason := s.reach(row)
	out := UserOrder{
		OrderID:       row.ID.String(),
		ClientOrderID: row.ClientOrderID,
		Market:        marketRef,
		Type:          row.Type,
		TimeInForce:   row.TimeInForce,
		Status:        row.Status,
		CreatedAt:     row.CreatedAt,
		Open:          row.MarketID != nil,
		Price:         row.Price,
		Remaining:     row.ORemainingHaveAmount,
		Reachable:     market != nil,
		Unreachable:   reason,
	}
	if row.Side != nil {
		out.Side = *row.Side
	}
	return out
}

// rowMarketRef finds the market from an order
func (s *Service) rowMarketRef(row *repository.OrderRow) (string, error) {
	if row.MarketID != nil {
		return s.marketRef(*row.MarketID)
	}
	if row.Status != repository.OrderStatusPending {
		return "", errNotLive
	}
	for _, m := range s.cache.GetMarkets() {
		if (m.BaseInstrumentID == row.HaveInstrumentID && m.QuoteInstrumentID == row.WantInstrumentID) ||
			(m.BaseInstrumentID == row.WantInstrumentID && m.QuoteInstrumentID == row.HaveInstrumentID) {
			return utils.MergeMarketRef(m.BaseSymbol, m.QuoteSymbol), nil
		}
	}
	return "", repository.ErrMarketNotFound
}

func (s *Service) marketRef(marketID int) (string, error) {
	market, err := s.cache.GetMarketByID(marketID)
	if err != nil {
		return "", err
	}
	return utils.MergeMarketRef(market.BaseSymbol, market.QuoteSymbol), nil
}

func (s *Service) lookupUser(ctx context.Context, username string) (*repository.User, error) {
	user, err := s.users.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	return user, nil
}

func (s *Service) Pause(ctx context.Context, marketRef string) error {
	market, ok := s.markets[marketRef]
	if !ok {
		return ErrMarketNotServed
	}
	market.Pause()
	return nil
}

func (s *Service) Resume(ctx context.Context, marketRef string) error {
	market, ok := s.markets[marketRef]
	if !ok {
		return ErrMarketNotServed
	}
	market.Resume()
	return nil
}

func (s *Service) Status(ctx context.Context) []MarketStatus {
	out := make([]MarketStatus, 0, len(s.markets))
	for ref, market := range s.markets {
		out = append(out, MarketStatus{Market: ref, Paused: market.IsPaused()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Market < out[j].Market })
	return out
}
