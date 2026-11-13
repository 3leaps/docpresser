package cmd

import (
	"fmt"

	"github.com/fulmenhq/gofulmen/foundry"
	"github.com/spf13/cobra"

	"github.com/3leaps/docpresser/internal/runner"
)

func newRunCmd() *cobra.Command {
	var packDir string
	var profile string
	var runID string
	var subjects []string
	var baselines []string
	var outDir string

	c := &cobra.Command{
		Use:   "run",
		Short: "Execute a pack against target and baseline subject drivers under containment supervision",
		RunE: func(cmd *cobra.Command, args []string) error {
			if packDir == "" || outDir == "" || (len(subjects) == 0 && len(baselines) == 0) {
				return commandFailure(foundry.ExitInvalidArgument, fmt.Errorf("--pack, --out, and at least one --subject or --baseline are required"))
			}
			manifest, err := runner.Run(cmd.Context(), runner.Options{
				PackDir:       packDir,
				Profile:       profile,
				RunID:         runID,
				SubjectSpecs:  subjects,
				BaselineSpecs: baselines,
				OutDir:        outDir,
			})
			if err != nil {
				return commandFailure(foundry.ExitFailure, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Completed run %s: %d observations written to %s (observations_sha256=%s)\n",
				manifest.RunID, manifest.ObservationCount, outDir, manifest.ObservationsSHA256)
			return nil
		},
	}
	c.Flags().StringVar(&packDir, "pack", "", "pack directory to execute")
	c.Flags().StringVar(&profile, "profile", "", "qualification profile label (defaults to pack tier)")
	c.Flags().StringVar(&runID, "run-id", "", "optional run identifier")
	c.Flags().StringArrayVar(&subjects, "subject", nil, "target subject driver specification (<name>=<command>)")
	c.Flags().StringArrayVar(&baselines, "baseline", nil, "baseline subject driver specification (<name>=<command>)")
	c.Flags().StringVar(&outDir, "out", "", "output evidence bundle directory")
	return c
}
