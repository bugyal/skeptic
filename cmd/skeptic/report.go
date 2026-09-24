package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/skeptic-labs/skeptic/internal/report"
	"github.com/spf13/cobra"
)

func newReportCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "report <run-dir-or-json>",
		Short: "Re-render a previous run",
		Long: "Renders a saved report as a table, as markdown ready to paste into an\n" +
			"upstream issue, or as the raw JSON.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := report.Load(args[0])
			if err != nil {
				return err
			}
			switch format {
			case "table":
				rep.Table(os.Stdout)
			case "md", "markdown":
				rep.Markdown(os.Stdout)
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(rep)
			default:
				return fmt.Errorf("--format must be table, md or json, got %q", format)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "table, md or json")
	return cmd
}
