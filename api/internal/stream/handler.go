package stream

import (
	"bufio"
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

type StreamHandler struct {
	logger    *logger.Logger
	marketHub *Hub
	markets   map[string]struct{} // served market refs, for validating :market
}

// MarketStream is the SSE endpoint GET /api/v1/stream/:market. It authenticates the connection (so the
// user id is known for the private stream added in C2), validates the market, then streams the
// public book and trade tape: the first frame is a full snapshot of the current cached book, then
// live book deltas, trades, and heartbeats. The book cache is served from memory — no DB read.
func (h *StreamHandler) MarketStream(c fiber.Ctx) error {
	market := c.Params("market")
	if _, ok := h.markets[market]; !ok {
		return utils.NewErrorResponse(c, fiber.StatusNotFound, "unknown market")
	}

	cl := &marketclient{
		market: market,
		// userID: middleware.UserIDFromContext(c),
		ch: make(chan []byte, clientSendBuffer),
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

func NewMarketsStreamHandler(log *logger.Logger, hub *Hub, markets []string) *StreamHandler {
	if log == nil {
		panic("logger cannot be nil")
	}
	if hub == nil {
		panic("hub cannot be nil")
	}
	set := make(map[string]struct{}, len(markets))
	for _, m := range markets {
		set[m] = struct{}{}
	}
	return &StreamHandler{logger: log, marketHub: hub, markets: set}
}
