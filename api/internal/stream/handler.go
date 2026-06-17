package stream

import (
	"bufio"
	"errors"
	"strconv"
	"time"

	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
)

// clientPingInterval is a backstop keepalive: core heartbeats (every couple of seconds) normally
// keep the connection warm and surface a dead client on the next flush, but if core goes silent this
// comment frame still detects a vanished client.
const clientPingInterval = 15 * time.Second

var errInvalidGroup = errors.New("group must be a positive multiple of the market price quantum")

type StreamHandler struct {
	logger    *logger.Logger
	marketHub *Hub
	markets   map[string]uint64 // served market ref -> price_quantum (validates :market and grouping)
}

// MarketStream is the SSE endpoint GET /api/v1/stream/:market. It authenticates the connection (so the
// user id is known for the private stream added in C2), validates the market, then streams the
// public book and trade tape: the first frame is a full snapshot of the current cached book, then
// live book deltas, trades, and heartbeats. The book cache is served from memory — no DB read.
func (h *StreamHandler) MarketStream(c fiber.Ctx) error {
	market := c.Params("market")
	priceQuantum, ok := h.markets[market]
	if !ok {
		return utils.NewErrorResponse(c, fiber.StatusNotFound, "unknown market")
	}

	group, err := parseGroup(c.Query("group"), priceQuantum)
	if err != nil {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	cl := &marketclient{
		market: market,
		group:  group,
		ch:     make(chan []byte, clientSendBuffer),
	}
	h.marketHub.connect(cl)

	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")

	return c.SendStreamWriter(func(w *bufio.Writer) {
		// Always deregister: closes/cleans the client whether it left or the Hub dropped it.
		defer h.marketHub.disconnect(cl)
		ping := time.NewTicker(clientPingInterval)
		defer ping.Stop()

		for {
			select {
			case frame, ok := <-cl.ch:
				if !ok {
					return // Hub closed us (slow consumer or shutdown)
				}
				if !flush(w, frame) {
					return // client gone
				}
			case <-ping.C:
				if !flush(w, []byte(": ping\n\n")) {
					return
				}
			}
		}
	})
}

func (h *StreamHandler) UserStream(c fiber.Ctx) error {
	cl := &userclient{
		userID: middleware.UserIDFromContext(c),
		ch:     make(chan []byte, clientSendBuffer),
	}
	h.marketHub.connect(cl)

	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")

	return c.SendStreamWriter(func(w *bufio.Writer) {
		// Always deregister: closes/cleans the client whether it left or the Hub dropped it.
		defer h.marketHub.disconnect(cl)
		ping := time.NewTicker(clientPingInterval)
		defer ping.Stop()

		for {
			select {
			case frame, ok := <-cl.ch:
				if !ok {
					return // Hub closed us (slow consumer or shutdown)
				}
				if !flush(w, frame) {
					return // client gone
				}
			case <-ping.C:
				if !flush(w, []byte(": ping\n\n")) {
					return
				}
			}
		}
	})
}

// flush writes one frame and pushes it to the socket, reporting false if the client has gone away.
func flush(w *bufio.Writer, frame []byte) bool {
	if _, err := w.Write(frame); err != nil {
		return false
	}
	return w.Flush() == nil
}

// parseGroup resolves the requested price-bucket size from the ?group query. Empty means native
// resolution (the market's price_quantum). Otherwise it must be a positive multiple of price_quantum
// — price_quantum is the tick, so you can only aggregate up from it (docs/event-log.md §5).
func parseGroup(raw string, priceQuantum uint64) (uint64, error) {
	if priceQuantum == 0 {
		priceQuantum = 1 // defensive; markets always have a positive quantum
	}
	if raw == "" {
		return priceQuantum, nil
	}
	g, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || g == 0 || g%priceQuantum != 0 {
		return 0, errInvalidGroup
	}
	return g, nil
}

// NewMarketsStreamHandler builds the SSE handler. markets maps each served market ref to its
// price_quantum, used to validate the :market param and the requested grouping.
func NewMarketsStreamHandler(log *logger.Logger, hub *Hub, markets map[string]uint64) *StreamHandler {
	if log == nil {
		panic("logger cannot be nil")
	}
	if hub == nil {
		panic("hub cannot be nil")
	}
	return &StreamHandler{logger: log, marketHub: hub, markets: markets}
}
