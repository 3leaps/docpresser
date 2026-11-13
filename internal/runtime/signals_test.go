package runtime

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/fulmenhq/gofulmen/signals"
)

func TestSignalControlRetainsImmediateSecondSignalAcrossHandoff(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		t.Run("handoff", func(t *testing.T) {
			manager := signals.NewManager()
			mirrored := make(chan os.Signal, 2)
			registered := make(chan struct{})
			control, err := startSignalControl(context.Background(), manager, func() (<-chan os.Signal, func()) {
				close(registered)
				return mirrored, func() {}
			})
			if err != nil {
				t.Fatalf("start signal control: %v", err)
			}
			defer control.Stop()

			select {
			case <-registered:
			case <-time.After(time.Second):
				t.Fatal("second-signal listener was not registered before Fulmen")
			}
			injector := signals.NewInjector(manager)
			if err := injector.WaitForListen(time.Second); err != nil {
				t.Fatalf("wait for listener: %v", err)
			}
			mirrored <- syscall.SIGTERM
			if err := injector.InjectWithContext(context.Background(), syscall.SIGTERM); err != nil {
				t.Fatalf("inject first signal: %v", err)
			}
			mirrored <- syscall.SIGTERM
			select {
			case <-control.Context.Done():
			case <-time.After(time.Second):
				t.Fatal("first signal did not cancel the command context")
			}
			select {
			case <-control.Interrupted:
			case <-time.After(time.Second):
				t.Fatal("first signal did not mark the command interrupted")
			}
			select {
			case <-control.Forced:
			case <-time.After(time.Second):
				t.Fatal("immediate second signal was lost during listener handoff")
			}
		})
	}
}
