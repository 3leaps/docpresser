package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/fulmenhq/gofulmen/foundry"
	"github.com/spf13/cobra"

	"github.com/3leaps/docpresser/internal/generate"
	"github.com/3leaps/docpresser/internal/oracle"
	"github.com/3leaps/docpresser/internal/pack"
)

func newBindCorpusCmd() *cobra.Command {
	var corpusPath string
	var tier string
	var outDir string

	c := &cobra.Command{
		Use:   "bind-corpus",
		Short: "Hydrate and bind a 3leaps/structcorpus checkout into an offline pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			if corpusPath == "" || tier == "" || outDir == "" {
				return commandFailure(foundry.ExitInvalidArgument, fmt.Errorf("--corpus, --tier, and --out are required"))
			}
			loaded, err := pack.BindCorpus(cmd.Context(), corpusPath, tier, outDir)
			if err != nil {
				return commandFailure(foundry.ExitDataInvalid, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Bound %d cases (tier=%s, sha256=%s) to %s\n", loaded.Pack.CaseCount, loaded.Pack.Tier, loaded.PackSHA256, outDir)
			return nil
		},
	}
	c.Flags().StringVar(&corpusPath, "corpus", "", "path to 3leaps/structcorpus checkout")
	c.Flags().StringVar(&tier, "tier", "smoke", "structcorpus tier (smoke|conformance|stress)")
	c.Flags().StringVar(&outDir, "out", "", "output pack directory")
	return c
}

func newGenerateCmd() *cobra.Command {
	var seed uint64
	var scenario string
	var scale string
	var outDir string

	c := &cobra.Command{
		Use:   "generate",
		Short: "Generate a deterministic synthetic workload pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			if scenario == "" || outDir == "" {
				return commandFailure(foundry.ExitInvalidArgument, fmt.Errorf("--scenario and --out are required"))
			}
			loaded, err := generate.Generate(generate.Options{
				Seed:     seed,
				Scenario: scenario,
				Scale:    scale,
				OutDir:   outDir,
			})
			if err != nil {
				return commandFailure(foundry.ExitDataInvalid, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Generated %d cases (scenario=%s, seed=%d, sha256=%s) in %s\n",
				loaded.Pack.CaseCount, scenario, seed, loaded.PackSHA256, outDir)
			return nil
		},
	}
	c.Flags().Uint64Var(&seed, "seed", 0, "deterministic SplitMix64 seed")
	c.Flags().StringVar(&scenario, "scenario", "", "workload scenario (k8s-bundle|openapi-graph|diabolical-alias-pyramid|yaml11-migration|synthetic-workload)")
	c.Flags().StringVar(&scale, "scale", "small", "workload scale (small|medium|large)")
	c.Flags().StringVar(&outDir, "out", "", "output pack directory")
	return c
}

func newValidateCmd() *cobra.Command {
	var packDir string

	c := &cobra.Command{
		Use:   "validate",
		Short: "Validate a bound or generated pack directory against docpresser/v0 schemas and digests",
		RunE: func(cmd *cobra.Command, args []string) error {
			if packDir == "" {
				return commandFailure(foundry.ExitInvalidArgument, fmt.Errorf("--pack is required"))
			}
			loaded, err := pack.LoadAndValidatePack(packDir)
			if err != nil {
				return commandFailure(foundry.ExitDataInvalid, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Valid pack: kind=%s tier=%s cases=%d sha256=%s\n",
				loaded.Pack.PackKind, loaded.Pack.Tier, loaded.Pack.CaseCount, loaded.PackSHA256)
			return nil
		},
	}
	c.Flags().StringVar(&packDir, "pack", "", "pack directory to validate")
	return c
}

func newVerifyCmd() *cobra.Command {
	var runDir string
	var packDir string

	c := &cobra.Command{
		Use:   "verify",
		Short: "Verify an evidence bundle directory against its source pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runDir == "" || packDir == "" {
				return commandFailure(foundry.ExitInvalidArgument, fmt.Errorf("both --run and --pack are required"))
			}
			report, err := oracle.Verify(cmd.Context(), runDir, packDir)
			if err != nil {
				return commandFailure(foundry.ExitDataInvalid, err)
			}
			encoded, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", string(encoded))
			if report.Verdict != "pass" {
				return commandFailure(foundry.ExitFailure, fmt.Errorf("verification verdict: %s (%d findings)", report.Verdict, len(report.Findings)))
			}
			return nil
		},
	}
	c.Flags().StringVar(&runDir, "run", "", "evidence bundle directory")
	c.Flags().StringVar(&packDir, "pack", "", "source pack directory")
	return c
}

func newSummarizeCmd() *cobra.Command {
	var runDir string
	var packDir string
	var format string

	c := &cobra.Command{
		Use:   "summarize",
		Short: "Render a JSON or Markdown qualification summary for an evidence bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runDir == "" || packDir == "" {
				return commandFailure(foundry.ExitInvalidArgument, fmt.Errorf("both --run and --pack are required"))
			}
			report, err := oracle.Verify(cmd.Context(), runDir, packDir)
			if err != nil {
				return commandFailure(foundry.ExitDataInvalid, err)
			}
			switch format {
			case "json":
				encoded, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", string(encoded))
			case "markdown", "md":
				_, _ = fmt.Fprint(cmd.OutOrStdout(), oracle.SummarizeMarkdown(*report))
			default:
				return commandFailure(foundry.ExitInvalidArgument, fmt.Errorf("unsupported --format %q (use json or markdown)", format))
			}
			return nil
		},
	}
	c.Flags().StringVar(&runDir, "run", "", "evidence bundle directory")
	c.Flags().StringVar(&packDir, "pack", "", "source pack directory")
	c.Flags().StringVar(&format, "format", "markdown", "output format (json|markdown)")
	return c
}
