package observability

import (
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/alex99y/matching-engine/common/pkg/logger"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var ErrPrometheusServerAlreadyStarted = errors.New(
	"prometheus server is already started",
)

type PrometheusServer struct {
	metricsPort int
	metrics     *PrometheusMetrics
	logger      *logger.Logger
	app         *fiber.App
	isRunning   bool
}

func NewPrometheusServer(
	metricsPort int,
	metrics *PrometheusMetrics,
	logger *logger.Logger,
) *PrometheusServer {
	app := fiber.New()

	handler := promhttp.HandlerFor(
		metrics.GetRegistry(),
		promhttp.HandlerOpts{},
	)
	app.Get("/metrics", adaptor.HTTPHandler(handler))

	return &PrometheusServer{
		metricsPort: metricsPort,
		metrics:     metrics,
		logger:      logger,
		app:         app,
	}
}

func (s *PrometheusServer) Start() error {
	if s.isRunning {
		return ErrPrometheusServerAlreadyStarted
	}

	address := fmt.Sprintf(":%d", s.metricsPort)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("prometheus metrics server bind %s: %w", address, err)
	}

	s.isRunning = true
	if s.logger != nil {
		s.logger.Info(
			fmt.Sprintf(
				"starting prometheus metrics server on %s", listener.Addr().String(),
			),
		)
	}

	go func() {
		serveErr := s.app.Listener(listener)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			if s.logger != nil {
				s.logger.Error(
					fmt.Sprintf(
						"prometheus metrics server stopped with error: %v", serveErr,
					),
				)
			}
		}
		s.isRunning = false
	}()

	return nil
}

func (s *PrometheusServer) Stop() error {
	if !s.isRunning {
		return nil
	}

	s.isRunning = false
	if s.logger != nil {
		s.logger.Info(
			fmt.Sprintf(
				"stopping prometheus metrics server on :%d", s.metricsPort,
			),
		)
	}
	return s.app.Shutdown()
}
