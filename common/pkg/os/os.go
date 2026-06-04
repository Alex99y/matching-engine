package os

import (
	"os"
	"os/signal"
	"syscall"
)

type OnQuit func()

func OnSigIntAndTerm() (chan os.Signal, OnQuit) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	return quit, func() {
		signal.Stop(quit)
	}
}
