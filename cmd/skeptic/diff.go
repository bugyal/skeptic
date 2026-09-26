package main

import (
	"fmt"
	"os"

	"github.com/bugyal/skeptic/internal/report"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "diff <old> <new>",
		Short: "Compare two runs and show what changed",
		Long: "Compares two reports (a report.json, or a run directory) task by task.\n" +
			"Tasks that gained a failure mode are regressions and make the exit status 1.\n" +
			"A task that moved to or from ERROR or UNSUPPORTED is listed apart and never\n" +
			"counts as a regression: an ERROR is usually the machine, not the benchmark.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			old, err := report.Load(args[0])
			if err != nil {
				return fmt.Errorf("old run: %w", err)
			}
			cur, err := report.Load(args[1])
			if err != nil {
				return fmt.Errorf("new run: %w", err)
			}
			d := report.Compare(old, cur, args[0], args[1])
			switch format {
			case "table":
				d.Table(os.Stdout)
			case "md", "markdown":
				d.Markdown(os.Stdout)
			case "json":
				if err := d.JSON(os.Stdout); err != nil {
					return err
				}
			default:
				return fmt.Errorf("--format must be table, md or json, got %q", format)
			}
			if d.HasRegressions() {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "table, md or json")
	return cmd
}
