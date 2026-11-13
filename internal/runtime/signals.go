package runtime

import (
	"context"
	"errors"
	"os"
	ossignal "os/signal"
	"sync"
	"syscall"

	"github.com/fulmenhq/gofulmen/signals"
)

// SignalControl binds SIGINT and SIGTERM to a cancelable command context.
type SignalControl struct {
	Context     context.Context
	Interrupted <-chan struct{}
	Forced      <-chan struct{}

	stop func()
}

// StartSignalControl starts a Fulmen listener for the first shutdown signal.
func StartSignalControl(parent context.Context, manager *signals.Manager) (*SignalControl, error) {
	return startSignalControl(parent, manager, func() (<-chan os.Signal, func()) {
		channel := make(chan os.Signal, 2)
		ossignal.Notify(channel, syscall.SIGINT, syscall.SIGTERM)
		return channel, func() { ossignal.Stop(channel) }
	})
}

func startSignalControl(parent context.Context, manager *signals.Manager, rearm func() (<-chan os.Signal, func())) (*SignalControl, error) {
	if manager == nil {
		return nil, errors.New("signal manager is required")
	}
	ctx, cancel := context.WithCancel(parent)
	listenerCtx, stopListener := context.WithCancel(context.Background())
	interrupted := make(chan struct{})
	forced := make(chan struct{})
	var first, second, stopOnce sync.Once
	onSignal := func() {
		wasFirst := false
		first.Do(func() {
			wasFirst = true
			close(interrupted)
			cancel()
		})
		if !wasFirst {
			second.Do(func() { close(forced) })
		}
	}
	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGTERM} {
		if _, err := manager.Handle(sig, func(context.Context, os.Signal) error {
			onSignal()
			return nil
		}); err != nil {
			stopListener()
			cancel()
			return nil, err
		}
	}
	channel, stopRearmed := rearm()

	listenerDone := make(chan struct{})
	go func() {
		defer close(listenerDone)
		defer stopRearmed()
		_ = manager.Listen(listenerCtx)
		manager.Stop()
		if listenerCtx.Err() != nil {
			return
		}
		select {
		case <-channel:
		default:
		}
		for {
			select {
			case <-listenerCtx.Done():
				return
			case <-channel:
				onSignal()
			}
		}
	}()

	return &SignalControl{
		Context:     ctx,
		Interrupted: interrupted,
		Forced:      forced,
		stop: func() {
			stopOnce.Do(func() {
				stopListener()
				cancel()
				manager.Stop()
				<-listenerDone
			})
		},
	}, nil
}

// Stop cancels the command context and waits for listeners to end.
func (c *SignalControl) Stop() {
	if c != nil && c.stop != nil {
		c.stop()
	}
}
