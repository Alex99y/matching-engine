package stream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/marketdata"
	"github.com/alex99y/matching-engine/common/pkg/rabbitmq"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

// CandleSeeder is the only DB operation the CandleHub needs: seed the forming bucket on connect.
type CandleSeeder interface {
	GetCurrentCandle(ctx context.Context, marketID int, bucketStart time.Time) (*repository.Candle, error)
}

// CandleHub is a single-goroutine actor that:
//   - receives trade and heartbeat events from RabbitMQ
//   - on each trade: broadcasts a candle.trade frame to all clients of that market, and emits
//     candle.closed when the trade crosses a bucket boundary for a given interval
//   - on each heartbeat: emits candle.closed for any interval whose bucket expired without trades
//   - on client connect: seeds the forming bucket from DB (REPEATABLE READ) so there is no gap
//     between historical data and the live stream
type CandleHub struct {
	logger     *logger.Logger
	source     eventSource
	db         CandleSeeder
	marketIDs  map[string]int                                   // market ref -> DB market_id
	clients    map[string]map[int64]map[*candleClient]struct{}  // market -> interval -> clients
	buckets    map[string]map[int64]int64                       // market -> interval -> current bucketStart (unix sec)
	events     chan event
	register   chan *candleClient
	unregister chan *candleClient
	done       chan struct{}
}

func (h *CandleHub) Run(ctx context.Context) {
	go h.consume(ctx)
	h.loop(ctx)
}

func (h *CandleHub) consume(ctx context.Context) {
	err := h.source.Subscribe(ctx, func(d rabbitmq.ExchangeDelivery) {
		env, err := marketdata.ParseEnvelope(d.Body)
		if err != nil {
			h.logger.Error(fmt.Sprintf("candle hub: malformed envelope rk=%s: %v", d.RoutingKey, err))
			return
		}
		select {
		case h.events <- event{routingKey: d.RoutingKey, envelope: env}:
		case <-ctx.Done():
		}
	})
	if err != nil {
		h.logger.Error(fmt.Sprintf("candle hub subscriber: %v", err))
	}
}

func (h *CandleHub) loop(ctx context.Context) {
	defer close(h.done)
	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return
		case e := <-h.events:
			h.handleEvent(e)
		case cl := <-h.register:
			h.handleRegister(cl)
		case cl := <-h.unregister:
			h.handleUnregister(cl)
		}
	}
}

func (h *CandleHub) handleEvent(e event) {
	env := e.envelope

	switch env.Type {
	case marketdata.EventTrade:
		var t marketdata.Trade
		if err := env.Decode(&t); err != nil {
			h.logger.Error(fmt.Sprintf("candle hub: decode trade %s: %v", env.Market, err))
			return
		}
		tradeSec := env.Ts / 1000
		h.dispatchTrade(env.Market, tradeSec, t)

	case marketdata.EventHeartbeat:
		h.checkBuckets(env.Market, time.Now().Unix())
	}
}

// dispatchTrade broadcasts a candle.trade frame to all clients of the market, then checks
// every interval for a bucket boundary crossing and emits candle.closed if one occurred.
func (h *CandleHub) dispatchTrade(market string, tradeSec int64, t marketdata.Trade) {
	marketClients := h.clients[market]
	if len(marketClients) == 0 {
		return
	}

	frame := candleTradeFrame(tradeSec, t)
	for _, clients := range marketClients {
		for cl := range clients {
			h.send(cl, frame)
		}
	}

	for interval, clients := range marketClients {
		if len(clients) == 0 {
			continue
		}
		newBucket := (tradeSec / interval) * interval
		prev := h.buckets[market][interval]
		if prev > 0 && newBucket > prev {
			closed := candleClosedFrame(interval, prev)
			for cl := range clients {
				h.send(cl, closed)
			}
			h.buckets[market][interval] = newBucket
		}
	}
}

// checkBuckets is called on heartbeat to close idle buckets that received no trades.
func (h *CandleHub) checkBuckets(market string, nowSec int64) {
	for interval, clients := range h.clients[market] {
		if len(clients) == 0 {
			continue
		}
		newBucket := (nowSec / interval) * interval
		prev := h.buckets[market][interval]
		if prev > 0 && newBucket > prev {
			closed := candleClosedFrame(interval, prev)
			for cl := range clients {
				h.send(cl, closed)
			}
			h.buckets[market][interval] = newBucket
		}
	}
}

// handleRegister adds the client, then queries the DB for the forming bucket under REPEATABLE READ.
// Because this runs on the Hub goroutine (single-threaded), the DB snapshot is taken while the
// events channel is not being drained. Any trade events that arrived between the DB query start
// and when the Hub resumes are strictly in the channel buffer — no overlap, no gap.
func (h *CandleHub) handleRegister(cl *candleClient) {
	if _, ok := h.clients[cl.market]; !ok {
		close(cl.ch)
		return
	}

	if h.clients[cl.market][cl.interval] == nil {
		h.clients[cl.market][cl.interval] = make(map[*candleClient]struct{})
	}
	h.clients[cl.market][cl.interval][cl] = struct{}{}

	now := time.Now().Unix()
	bucketStart := (now / cl.interval) * cl.interval

	if h.buckets[cl.market][cl.interval] == 0 {
		h.buckets[cl.market][cl.interval] = bucketStart
	}

	seedCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	candle, err := h.db.GetCurrentCandle(seedCtx, h.marketIDs[cl.market], time.Unix(bucketStart, 0).UTC())
	if err != nil && !errors.Is(err, repository.ErrNoCandle) {
		h.logger.Error(fmt.Sprintf("candle hub: seed market=%s interval=%d: %v", cl.market, cl.interval, err))
	}

	h.send(cl, candleSnapshotFrame(cl.interval, bucketStart, candle))
}

func (h *CandleHub) handleUnregister(cl *candleClient) {
	clients := h.clients[cl.market][cl.interval]
	if _, ok := clients[cl]; !ok {
		return
	}
	delete(clients, cl)
	close(cl.ch)
}

func (h *CandleHub) send(cl *candleClient, frame []byte) {
	if frame == nil {
		return
	}
	select {
	case cl.ch <- frame:
	default:
		h.handleUnregister(cl)
	}
}

func (h *CandleHub) closeAll() {
	for _, intervals := range h.clients {
		for _, clients := range intervals {
			for cl := range clients {
				delete(clients, cl)
				close(cl.ch)
			}
		}
	}
}

func (h *CandleHub) connect(cl *candleClient) {
	select {
	case h.register <- cl:
	case <-h.done:
		close(cl.ch)
	}
}

func (h *CandleHub) disconnect(cl *candleClient) {
	select {
	case h.unregister <- cl:
	case <-h.done:
	}
}

func NewCandleHub(
	rmqClient *rabbitmq.RabbitMQClient,
	markets []string,
	marketIDs map[string]int,
	db CandleSeeder,
	log *logger.Logger,
) (*CandleHub, error) {
	if log == nil {
		panic("logger cannot be nil")
	}

	patterns := make([]string, 0, len(markets))
	for _, m := range markets {
		patterns = append(patterns, marketdata.MarketBinding(m))
	}
	sub, err := rabbitmq.NewSubscriber(rmqClient, rabbitmq.SubscriberArgs{
		Exchange:     marketdata.ExchangeName,
		ExchangeKind: rabbitmq.ExchangeKindTopic,
		Patterns:     patterns,
	}, log)
	if err != nil {
		return nil, fmt.Errorf("candle hub: %w", err)
	}

	h := &CandleHub{
		logger:     log,
		source:     sub,
		db:         db,
		marketIDs:  marketIDs,
		clients:    make(map[string]map[int64]map[*candleClient]struct{}, len(markets)),
		buckets:    make(map[string]map[int64]int64, len(markets)),
		events:     make(chan event, eventBuffer),
		register:   make(chan *candleClient),
		unregister: make(chan *candleClient),
		done:       make(chan struct{}),
	}
	for _, m := range markets {
		h.clients[m] = make(map[int64]map[*candleClient]struct{})
		h.buckets[m] = make(map[int64]int64)
	}
	return h, nil
}
