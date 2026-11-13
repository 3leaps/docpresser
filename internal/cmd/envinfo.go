package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/fulmenhq/gofulmen/appidentity"
	"github.com/fulmenhq/gofulmen/crucible"
)

func newEnvinfoCmd(identity *appidentity.Identity) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "envinfo",
		Short: "Display environment information",
		Long:  `Display comprehensive environment, configuration, and version information.`,
		Run: func(cmd *cobra.Command, args []string) {
			version := crucible.GetVersion()

			loggerInstance.Info(fmt.Sprintf("=== %s Environment Information ===", identity.BinaryName))
			loggerInstance.Info("")

			loggerInstance.Info("Application:")
			loggerInstance.Info("  Name:        "+identity.BinaryName, zap.String("binary_name", identity.BinaryName))
			loggerInstance.Info("  Vendor:      "+identity.Vendor, zap.String("vendor", identity.Vendor))
			loggerInstance.Info("  Description: "+identity.Description, zap.String("description", identity.Description))
			loggerInstance.Info("  Version:     "+getVersionString(), zap.String("version", getVersionString()))
			loggerInstance.Info("  Commit:      "+commit, zap.String("commit", commit))
			loggerInstance.Info("  Built:       "+buildDate, zap.String("build_date", buildDate))
			loggerInstance.Info("")

			loggerInstance.Info("Foundation Dependencies:")
			loggerInstance.Info("  Gofulmen:    v"+version.Gofulmen, zap.String("gofulmen_version", version.Gofulmen))
			loggerInstance.Info("  Fulmen Crucible: v"+version.Crucible, zap.String("fulmen_crucible_version", version.Crucible))
			loggerInstance.Info("")

			loggerInstance.Info("Runtime:")
			loggerInstance.Info("  Go Version:  "+runtime.Version(), zap.String("go_version", runtime.Version()))
			loggerInstance.Info("  GOOS:        "+runtime.GOOS, zap.String("goos", runtime.GOOS))
			loggerInstance.Info("  GOARCH:      "+runtime.GOARCH, zap.String("goarch", runtime.GOARCH))
			loggerInstance.Info(fmt.Sprintf("  NumCPU:      %d", runtime.NumCPU()), zap.Int("num_cpu", runtime.NumCPU()))
			loggerInstance.Info("")

			loggerInstance.Info("Configuration:")
			loggerInstance.Info("  Env Prefix:  "+identity.EnvPrefix, zap.String("env_prefix", identity.EnvPrefix))
			loggerInstance.Info("  Config Name: "+identity.ConfigName, zap.String("config_name", identity.ConfigName))

			loggerInstance.Info("")
			loggerInstance.Info("Environment Variables (presence only):")
			envVars := []string{
				identity.EnvPrefix + "CONFIG_PATH",
				identity.EnvPrefix + "RUN_ROOT",
			}
			for _, envVar := range envVars {
				status := "unset"
				if os.Getenv(envVar) != "" {
					status = "set"
				}
				loggerInstance.Info("  "+envVar+": "+status, zap.String("env_var", envVar), zap.String("status", status))
			}

			loggerInstance.Info("")
			loggerInstance.Info(fmt.Sprintf("=== End %s Environment Information ===", identity.BinaryName))
		},
	}

	return cmd
}

func getVersionString() string {
	if version != "" && version != "dev" {
		return version
	}
	return "dev"
}
