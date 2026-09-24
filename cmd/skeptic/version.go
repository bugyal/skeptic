package main

import (
	"fmt"
	"runtime"

	"github.com/bugyal/skeptic/internal/docker"
	"github.com/bugyal/skeptic/internal/report"
	"github.com/bugyal/skeptic/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, toolchain and detected Docker API",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("skeptic %s\n", version.Version)
			fmt.Printf("  commit:        %s\n", version.Revision())
			if version.Date != "" {
				fmt.Printf("  built:         %s\n", version.Date)
			}
			fmt.Printf("  go:            %s\n", version.GoVersion())
			fmt.Printf("  platform:      %s/%s\n", runtime.GOOS, runtime.GOARCH)
			fmt.Printf("  report schema: %d\n", report.SchemaVersion)

			api := docker.New(logger()).APIVersion(cmd.Context())
			if api == "" {
				api = "not detected"
			}
			fmt.Printf("  docker api:    %s\n", api)
			return nil
		},
	}
}
