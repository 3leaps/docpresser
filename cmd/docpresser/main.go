package main

import (
	"context"
	"fmt"
	"os"
	"time"

	appidentityembed "github.com/3leaps/docpresser/internal/assets/appidentity"
	"github.com/fulmenhq/gofulmen/appidentity"
	"github.com/fulmenhq/gofulmen/foundry"
	"github.com/fulmenhq/gofulmen/signals"
	"go.uber.org/zap"

	"github.com/3leaps/docpresser/internal/cmd"
	"github.com/3leaps/docpresser/internal/config"
	"github.com/3leaps/docpresser/internal/runtime"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := appidentityembed.Register(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register embedded app identity: %v\n", err)
		os.Exit(foundry.ExitConfigInvalid)
	}

	identity, err := appidentity.Get(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load app identity: %v\n", err)
		os.Exit(foundry.ExitConfigInvalid)
	}

	logger := runtime.SetupLogging(identity)

	manager := signals.NewManager()
	manager.OnShutdown(func(context.Context) error {
		logger.Info("shutting down gracefully")
		return nil
	})
	signalControl, err := runtime.StartSignalControl(ctx, manager)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register signal handler: %v\n", err)
		os.Exit(foundry.ExitFailure)
	}
	ctx = signalControl.Context
	defer signalControl.Stop()

	cfg, err := config.LoadConfig(ctx, identity, logger)
	if err != nil {
		logger.Error("failed to load config", zap.Error(err))
		os.Exit(foundry.ExitConfigInvalid)
	}

	cmd.SetVersionInfo(version, commit, buildDate)
	cmd.Initialize(ctx, identity, logger, cfg)

	commandDone := make(chan error, 1)
	go func() { commandDone <- cmd.Execute() }()
	var commandErr error
	select {
	case commandErr = <-commandDone:
	case <-signalControl.Interrupted:
		shutdownTimer := time.NewTimer(10 * time.Second)
		select {
		case commandErr = <-commandDone:
			if !shutdownTimer.Stop() {
				<-shutdownTimer.C
			}
		case <-shutdownTimer.C:
			commandErr = fmt.Errorf("%w: command did not finish graceful shutdown", context.DeadlineExceeded)
		case <-signalControl.Forced:
			if !shutdownTimer.Stop() {
				<-shutdownTimer.C
			}
			commandErr = context.Canceled
		}
	}
	if commandErr != nil {
		logger.Error("command execution failed", zap.Error(commandErr))
		os.Exit(cmd.ExitCode(commandErr))
	}

	os.Exit(foundry.ExitSuccess)
}
