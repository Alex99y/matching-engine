package client

import (
	"context"
	"net/http"
	"strconv"
)

type OrderSide string
type OrderType string
type TimeInForce string

const (
	Buy  OrderSide = "buy"
	Sell OrderSide = "sell"

	Limit  OrderType = "limit"
	Market OrderType = "market"

	GTC TimeInForce = "gtc"
	IOC TimeInForce = "ioc"
	FOK TimeInForce = "fok"
)

// NewOrder is one entry in a POST /orders batch. Price/Quantity are raw instrument quanta.
// QuoteQty is set only for a market buy (its spend budget); ExpiresAt only for GTC.
type NewOrder struct {
	ClientOrderID string      `json:"client_order_id,omitempty"`
	Side          OrderSide   `json:"order_side"`
	Type          OrderType   `json:"order_type"`
	TimeInForce   TimeInForce `json:"order_tif"`
	Market        string      `json:"market"`
	Price         uint64      `json:"price,omitempty"`
	Quantity      uint64      `json:"quantity,omitempty"`
	QuoteQty      *uint64     `json:"quote_qty,omitempty"`
	ExpiresAt     *int64      `json:"expires_at,omitempty"`
	PostOnly      bool        `json:"post_only,omitempty"`
}

type CreateResult struct {
	Index   int     `json:"index"`
	OrderID *string `json:"order_id,omitempty"`
	Error   *string `json:"error,omitempty"`
}

type CancelResult struct {
	OrderID string  `json:"order_id"`
	Error   *string `json:"error,omitempty"`
}

// OrderLeg holds the resting or cancelled sub-record. Have/Want map to base/quote by side.
type OrderLeg struct {
	Price         uint64 `json:"price,omitempty"`
	Side          string `json:"side,omitempty"`
	CancelledAt   int64  `json:"cancelled_at,omitempty"`
	RemainingHave uint64 `json:"remaining_have"`
	RemainingWant uint64 `json:"remaining_want"`
}

type Match struct {
	ID          string `json:"id"`
	Price       uint64 `json:"price"`
	BaseAmount  uint64 `json:"base_amount"`
	QuoteAmount uint64 `json:"quote_amount"`
	Fee         uint64 `json:"fee"`
	IsTaker     bool   `json:"is_taker"`
	MatchTime   int64  `json:"match_time"`
}

type Order struct {
	ID             string    `json:"id"`
	ClientOrderID  string    `json:"client_order_id,omitempty"`
	Type           string    `json:"type"`
	TimeInForce    string    `json:"time_in_force"`
	Side           *string   `json:"side,omitempty"`
	HaveQuantity   uint64    `json:"have_quantity"`
	WantQuantity   uint64    `json:"want_quantity"`
	CreatedAt      int64     `json:"created_at"`
	ExpiresAt      *int64    `json:"expires_at,omitempty"`
	OpenOrder      *OrderLeg `json:"open_order,omitempty"`
	CancelledOrder *OrderLeg `json:"cancelled_order,omitempty"`
	Matches        []Match   `json:"matches,omitempty"` // only populated by GetOrder
}

// OrdersFilter is the query for ListOrders. Zero values are omitted.
type OrdersFilter struct {
	ClientOrderID string
	Market        string
	ShowOpen      bool
	ShowCancelled bool
	Limit         int
}

type createResponse struct {
	Results []CreateResult `json:"results"`
}

type cancelResponse struct {
	Results []CancelResult `json:"results"`
}

type cancelRequest struct {
	OrderIDs []string `json:"order_ids"`
}

// CreateOrders submits a batch. 202 (all queued), 207 (partial), and 422 (all rejected) are
// all returned as results with a nil error — callers inspect each CreateResult.
func (c *Client) CreateOrders(ctx context.Context, token string, orders []NewOrder) ([]CreateResult, error) {
	var resp createResponse
	err := c.do(ctx, http.MethodPost, "/orders/", token, orders, &resp,
		http.StatusAccepted, http.StatusMultiStatus, http.StatusUnprocessableEntity)
	if err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// CreateOrder submits a single order and returns its server-assigned id. A synchronous
// per-order rejection (validation, unknown market) comes back as *OrderRejectedError; an
// async rejection (insufficient funds) still returns an id and surfaces on the order stream.
func (c *Client) CreateOrder(ctx context.Context, token string, order NewOrder) (string, error) {
	results, err := c.CreateOrders(ctx, token, []NewOrder{order})
	if err != nil {
		return "", err
	}
	if len(results) != 1 {
		return "", &OrderRejectedError{Reason: "expected exactly one result"}
	}
	r := results[0]
	if r.Error != nil {
		return "", &OrderRejectedError{Reason: *r.Error}
	}
	if r.OrderID == nil {
		return "", &OrderRejectedError{Reason: "no order id returned"}
	}
	return *r.OrderID, nil
}

func (c *Client) CancelOrders(ctx context.Context, token string, orderIDs []string) ([]CancelResult, error) {
	var resp cancelResponse
	err := c.do(ctx, http.MethodDelete, "/orders/", token, cancelRequest{OrderIDs: orderIDs}, &resp, http.StatusAccepted)
	if err != nil {
		return nil, err
	}
	return resp.Results, nil
}

func (c *Client) CancelOrder(ctx context.Context, token, orderID string) error {
	results, err := c.CancelOrders(ctx, token, []string{orderID})
	if err != nil {
		return err
	}
	if len(results) == 1 && results[0].Error != nil {
		return &OrderRejectedError{Reason: *results[0].Error}
	}
	return nil
}

// GetOrder fetches one order including its fills.
func (c *Client) GetOrder(ctx context.Context, token, orderID string) (Order, error) {
	var o Order
	err := c.do(ctx, http.MethodGet, "/orders/"+orderID, token, nil, &o, http.StatusOK)
	return o, err
}

func (c *Client) ListOrders(ctx context.Context, token string, f OrdersFilter) ([]Order, error) {
	q := map[string]string{
		"client_order_id": f.ClientOrderID,
		"market":          f.Market,
	}
	if f.ShowOpen {
		q["show_open"] = "true"
	}
	if f.ShowCancelled {
		q["show_cancelled"] = "true"
	}
	if f.Limit > 0 {
		q["limit"] = strconv.Itoa(f.Limit)
	}
	var out []Order
	err := c.do(ctx, http.MethodGet, "/orders/"+query(q), token, nil, &out, http.StatusOK)
	return out, err
}
