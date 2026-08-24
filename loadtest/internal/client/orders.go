package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

type OrderSide string
type OrderType string
type TimeInForce string

const (
	Sell OrderSide = "sell"
	Buy  OrderSide = "buy"
)

const (
	Limit           OrderType = "limit"
	MarketOrderType OrderType = "market"
)

const (
	GoodTillCancel    TimeInForce = "gtc"
	ImmediateOrCancel TimeInForce = "ioc"
	FillOrKill        TimeInForce = "fok"
)

var ErrOrderRejected = errors.New("order rejected by the api")

type CreateOrderRequest struct {
	ClientOrderID string      `json:"client_order_id,omitempty"`
	OrderSide     OrderSide   `json:"order_side"`
	OrderType     OrderType   `json:"order_type"`
	TimeInForce   TimeInForce `json:"order_tif"`
	Market        string      `json:"market"`
	Price         uint64      `json:"price"`
	Quantity      uint64      `json:"quantity"`
	QuoteQty      *uint64     `json:"quote_qty,omitempty"`
	ExpiresAt     *int64      `json:"expires_at,omitempty"`
}

type BatchCreateOrderResult struct {
	Index   int        `json:"index"`
	OrderID *uuid.UUID `json:"order_id,omitempty"`
	Error   *string    `json:"error,omitempty"`
}

type batchCreateOrderResponse struct {
	Results []BatchCreateOrderResult `json:"results"`
}

type cancelOrderRequest struct {
	OrderIDs []string `json:"order_ids"`
}

type BatchCancelOrderResult struct {
	OrderID string  `json:"order_id"`
	Error   *string `json:"error,omitempty"`
}

type batchCancelOrderResponse struct {
	Results []BatchCancelOrderResult `json:"results"`
}

// CreateOrders submits a batch and returns the per-index results as-is (mixed success/failure —
// the api replies 202/207/422 depending on the failure ratio, all of which are valid outcomes
// here since the caller inspects individual results anyway).
func (c *Client) CreateOrders(ctx context.Context, token string, reqs []CreateOrderRequest) ([]BatchCreateOrderResult, error) {
	var resp batchCreateOrderResponse
	err := c.do(ctx, http.MethodPost, "/orders/", token, reqs, &resp,
		http.StatusAccepted, http.StatusMultiStatus, http.StatusUnprocessableEntity)
	if err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// CreateOrder submits a single order and returns its id. The id is assigned synchronously by the
// api (before the matching engine ever sees the order) — it's the correlation key measurement
// code uses to match this send against the later stream event.
func (c *Client) CreateOrder(ctx context.Context, token string, req CreateOrderRequest) (uuid.UUID, error) {
	results, err := c.CreateOrders(ctx, token, []CreateOrderRequest{req})
	if err != nil {
		return uuid.UUID{}, err
	}
	if len(results) != 1 {
		return uuid.UUID{}, fmt.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != nil {
		return uuid.UUID{}, fmt.Errorf("%w: %s", ErrOrderRejected, *results[0].Error)
	}
	if results[0].OrderID == nil {
		return uuid.UUID{}, fmt.Errorf("%w: no order id returned", ErrOrderRejected)
	}
	return *results[0].OrderID, nil
}

func (c *Client) CancelOrders(ctx context.Context, token string, orderIDs []uuid.UUID) ([]BatchCancelOrderResult, error) {
	raw := make([]string, len(orderIDs))
	for i, id := range orderIDs {
		raw[i] = id.String()
	}
	var resp batchCancelOrderResponse
	err := c.do(ctx, http.MethodDelete, "/orders/", token, cancelOrderRequest{OrderIDs: raw}, &resp, http.StatusAccepted)
	if err != nil {
		return nil, err
	}
	return resp.Results, nil
}

func (c *Client) CancelOrder(ctx context.Context, token string, orderID uuid.UUID) error {
	results, err := c.CancelOrders(ctx, token, []uuid.UUID{orderID})
	if err != nil {
		return err
	}
	if len(results) != 1 {
		return fmt.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != nil {
		return fmt.Errorf("%w: %s", ErrOrderRejected, *results[0].Error)
	}
	return nil
}

type openOrder struct {
	ID uuid.UUID `json:"id"`
}

// GetOpenOrderIDs lists every currently-open order id for the account in marketRef — used by spam
// pool teardown to sweep up resting orders (e.g. maker legs a taker never crossed) so repeated
// test runs don't leave the book cluttered.
func (c *Client) GetOpenOrderIDs(ctx context.Context, token, marketRef string) ([]uuid.UUID, error) {
	var orders []openOrder
	path := "/orders/?show_open=true&market=" + marketRef
	if err := c.do(ctx, http.MethodGet, path, token, nil, &orders, http.StatusOK); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(orders))
	for i, o := range orders {
		ids[i] = o.ID
	}
	return ids, nil
}
