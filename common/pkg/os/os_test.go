package os_test

import (
	"os"
	"syscall"
	"testing"
	"time"

	meos "github.com/alex99y/matching-engine/common/pkg/os"
)

func TestOnSigIntAndTermReceivesSignal(t *testing.T) {
	quit, stop := meos.OnSigIntAndTerm()
	defer stop()

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}
	if err := proc.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("signal: %v", err)
	}

	select {
	case sig := <-quit:
		if sig != syscall.SIGINT {
			t.Errorf("got signal %v, want SIGINT", sig)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func TestOnSigIntAndTermStopUnregisters(t *testing.T) {
	quit, stop := meos.OnSigIntAndTerm()
	stop()

	// Sending another real signal after Stop would fall back to the OS default
	// disposition (process termination), so we can't safely re-signal here. Just
	// assert Stop doesn't panic and nothing is queued on the channel.
	select {
	case sig := <-quit:
		t.Errorf("unexpected signal after stop: %v", sig)
	default:
	}
}
