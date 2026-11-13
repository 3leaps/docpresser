package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fulmenhq/gofulmen/appidentity"
	"github.com/fulmenhq/gofulmen/logging"
	"github.com/spf13/cobra"

	"github.com/3leaps/docpresser/internal/config"
)

var logLevel string

var rootCmd *cobra.Command

var loggerInstance *logging.Logger

// Execute runs the root command.
func Execute() error {
	if rootCmd == nil {
		return fmt.Errorf("command is not initialized")
	}
	return rootCmd.Execute()
}

// Initialize sets up the root command with app identity, logger, and config.
func Initialize(ctx context.Context, identity *appidentity.Identity, logger *logging.Logger, cfg *config.Config) {
	_ = cfg
	loggerInstance = logger

	rootCmd = &cobra.Command{
		Use:   identity.BinaryName,
		Short: identity.Description,
		Long:  identity.Description,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if logLevel != "" {
				switch strings.ToUpper(logLevel) {
				case "TRACE":
					logger.SetLevel(logging.TRACE)
				case "DEBUG":
					logger.SetLevel(logging.DEBUG)
				case "INFO":
					logger.SetLevel(logging.INFO)
				case "WARN", "WARNING":
					logger.SetLevel(logging.WARN)
				case "ERROR":
					logger.SetLevel(logging.ERROR)
				default:
					return fmt.Errorf("invalid log level: %s (use trace|debug|info|warn|error)", logLevel)
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	rootCmd.SetContext(ctx)
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "info", "logging level (trace|debug|info|warn|error)")

	rootCmd.AddCommand(newVersionCmd(identity))
	rootCmd.AddCommand(newEnvinfoCmd(identity))
	rootCmd.AddCommand(newDoctorCmd(identity))
	rootCmd.AddCommand(newBindCorpusCmd())
	rootCmd.AddCommand(newGenerateCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newVerifyCmd())
	rootCmd.AddCommand(newSummarizeCmd())
}
