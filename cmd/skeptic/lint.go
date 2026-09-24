package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/skeptic-labs/skeptic/internal/lint"
	"github.com/skeptic-labs/skeptic/internal/report"
	"github.com/spf13/cobra"
)

func newLintCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "lint <path>",
		Short: "Static checks on task structure (no Docker, no model, no cost)",
		Long: "Checks task structure without building anything: manifests parse, files\n" +
			"exist, and — most importantly — the answer is not reachable by the agent,\n" +
			"either through the instruction text or through the Docker build context.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, err := registry().Discover(args[0])
			if err != nil {
				return err
			}

			results := make([]lint.Result, 0, len(tasks))
			var fails, warns int
			for _, t := range tasks {
				r := lint.Check(t)
				results = append(results, r)
				switch r.Worst {
				case lint.FAIL:
					fails++
				case lint.WARN:
					warns++
				}
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(results); err != nil {
					return err
				}
			} else {
				colour := report.IsTerminal(os.Stdout)
				for _, r := range results {
					fmt.Printf("%s  %s\n", paintSeverity(r.Worst, colour), r.TaskID)
					for _, f := range r.Findings {
						fmt.Printf("      %-5s %-11s %s\n", f.Severity, f.Check, f.Message)
					}
				}
				fmt.Printf("\n%d task(s) · %d fail · %d warn\n", len(results), fails, warns)
			}

			if fails > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit findings as JSON")
	return cmd
}

func paintSeverity(s lint.Severity, colour bool) string {
	txt := fmt.Sprintf("%-4s", s)
	if !colour {
		return txt
	}
	switch s {
	case lint.FAIL:
		return "\033[31m" + txt + "\033[0m"
	case lint.WARN:
		return "\033[33m" + txt + "\033[0m"
	default:
		return "\033[32m" + txt + "\033[0m"
	}
}
