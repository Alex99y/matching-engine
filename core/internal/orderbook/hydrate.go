package orderbook

import (
	"strings"
	"time"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

// This file is the book↔DB boundary: rebuilding resting orders from persisted rows on
// startup/recovery (Hydrate), mapping an order event to its repository insert params
// (DeriveInsertParams), and the engine/DB time-in-force enum bridge.

// Hydrate rebuilds the resting book from persisted open orders. Rows must be supplied
// in ascending open_orders.id order so per-price-level FIFO priority is preserved.
// The pre-restart original quantity is not persisted, so a hydrated order's Quantity
// is set to its remaining base amount (see CancelOrder for the consequence).
func (o *OrderBook) Hydrate(rows []repository.OpenOrderHydration) {
	for _, r := range rows {
		base := hydrateBase(r)
		event := &oeq.OpenOrderEvent{
			OrderID:     r.OrderID,
			UserID:      r.UserID,
			MarketID:    o.market.ID,
			Side:        oeq.OrderSide(r.Side),
			Type:        oeq.OrderType(r.Type),
			TimeInForce: tifFromDB(r.TimeInForce),
			Price:       r.Price,
			Quantity:    base,
			ExpiresAt:   r.ExpiresAt,
		}
		if r.ClientOrderID != nil {
			event.ClientOrderID = *r.ClientOrderID
		}
		o.rest(&Order{OpenOrder: event, Remaining: base})
	}
}

func hydrateBase(r repository.OpenOrderHydration) uint64 {
	if oeq.OrderSide(r.Side) == oeq.BuyOrder {
		return r.RemainingWantAmount // want = base for a buy
	}
	return r.RemainingHaveAmount // have = base for a sell
}

// DeriveInsertParams maps an order event + market to the repository insert params.
// Status is left empty for the caller to set from the matching outcome.
//
// Limit buy:  have=quote, want=base; have_qty = price×qty, want_qty = qty
// Limit sell: have=base,  want=quote; have_qty = qty,       want_qty = price×qty
// Market buy: quote-denominated; have_qty = quote_qty (want unknown until executed)
// Market sell: base-denominated;  have_qty = qty       (want unknown until executed)
func DeriveInsertParams(event *oeq.OpenOrderEvent, market *repository.Market) repository.InsertOrderParams {
	p := repository.InsertOrderParams{
		ID:          event.OrderID,
		UserID:      event.UserID,
		Type:        string(event.Type), // 'limit'/'market' already match the DB enum
		TimeInForce: tifToDB(event.TimeInForce),
	}

	if event.ClientOrderID != "" {
		p.ClientOrderID = &event.ClientOrderID
	}
	if event.ExpiresAt != nil {
		t := time.Unix(*event.ExpiresAt, 0).UTC()
		p.ExpiresAt = &t
	}

	if event.Side == oeq.BuyOrder {
		p.HaveInstrumentID = market.QuoteInstrumentID
		p.WantInstrumentID = market.BaseInstrumentID
	} else {
		p.HaveInstrumentID = market.BaseInstrumentID
		p.WantInstrumentID = market.QuoteInstrumentID
	}

	switch event.Type {
	case oeq.LimitOrder:
		notional := quoteAmount(event.Price, event.Quantity, market.BaseScale)
		qty := event.Quantity
		if event.Side == oeq.BuyOrder {
			p.HaveQuantity = &notional
			p.WantQuantity = &qty
		} else {
			p.HaveQuantity = &qty
			p.WantQuantity = &notional
		}
	case oeq.MarketOrder:
		if event.Side == oeq.BuyOrder {
			p.HaveQuantity = event.QuoteQty // quote budget
		} else {
			qty := event.Quantity
			p.HaveQuantity = &qty // base offered
		}
	}

	return p
}

// tifToDB / tifFromDB bridge the lowercase engine enum and the uppercase DB CHECK.
func tifToDB(t oeq.TimeInForce) string {
	switch t {
	case oeq.GoodTillCancel:
		return "GTC"
	case oeq.ImmediateOrCancel:
		return "IOC"
	case oeq.FillOrKill:
		return "FOK"
	default:
		return strings.ToUpper(string(t))
	}
}

func tifFromDB(s string) oeq.TimeInForce {
	switch s {
	case "GTC":
		return oeq.GoodTillCancel
	case "IOC":
		return oeq.ImmediateOrCancel
	case "FOK":
		return oeq.FillOrKill
	default:
		return oeq.TimeInForce(strings.ToLower(s))
	}
}
