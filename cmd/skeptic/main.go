// Command skeptic checks whether a coding-agent benchmark measures what it claims.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/skeptic-labs/skeptic/internal/adapter"
	"github.com/skeptic-labs/skeptic/internal/adapter/harbor"
	"github.com/spf13/cobra"
)

// exitInterrupted is the conventional shell status for death by SIGINT.
const exitInterrupted = 130

var verbosity int

func main() {
	// A cancelled context unwinds the run; container cleanup deliberately
	// uses its own context so Ctrl-C still tears down what it started.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := &cobra.Command{
		Use:   "skeptic",
		Short: "Check whether a coding-agent benchmark measures what it claims",
		Long: "Skeptic runs two control experiments on every task in a benchmark:\n" +
			"  oracle — apply the reference solution; the tests must score 1.0\n" +
			"  nop    — change nothing; the tests must score 0.0\n\n" +
			"Any task failing either control is flagged, with the evidence.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().CountVarP(&verbosity, "verbose", "v",
		"-v for per-step progress, -vv for container-level detail")

	root.AddCommand(newCheckCmd(), newLintCmd(), newReportCmd(), newVersionCmd())

	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "\ninterrupted")
			os.Exit(exitInterrupted)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// logger builds a slog logger whose level follows -v. Default is quiet:
// a CI run should print a table, not a narrative.
func logger() *slog.Logger {
	level := slog.LevelError
	switch {
	case verbosity == 1:
		level = slog.LevelInfo
	case verbosity >= 2:
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// registry returns the adapters compiled into this build, in priority order.
func registry() *adapter.Registry {
	return adapter.NewRegistry(harbor.New())
}
