// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"craftpack/pkg/cli"

	"github.com/spf13/cobra"
)

var errSilentSuccess = errors.New("silent success")

const rootHelpTemplateFormat = `craftpack v%s - Standardized Linux packaging factory for the SDP

USAGE:
  craftpack <COMMAND> [OPTIONS]

COMMANDS:
  build       Build system-compliant packages (.deb) from a craftpack.yml specification
  validate    Validate the syntax, schema, and paths of a craftpack.yml specification

GLOBAL OPTIONS:
  -h, --help             Display help information for the program or subcommand
  -V, --version          Display single-line version of the Craftpack utility
      --version-info     Display detailed build, compiler, and environment metadata
  -v, --verbose          Increase diagnostic logging verbosity (-v: DEBUG, -vv: TRACE)
  -q, --quiet            Quiet mode (suppresses all diagnostic outputs, showing only errors)
      --log-level <LVL>  Explicitly override and set the logging verbosity level
                         [possible values: trace, debug, info, warn, error]
                         [default: info] [env: CRAFTPACK_LOG_LEVEL]

EXAMPLES:
  # Build a Debian package with a specific release version
  craftpack build --spec craftpack.yml --target deb --package-version 1.4.2

  # Perform a dry-run validation of the workspace schema
  craftpack validate --spec config/craftpack.yml --strict
`

// NewRootCommand builds and configures the root Cobra command.
func NewRootCommand() *cobra.Command {
	var (
		showVersion     bool
		showVersionInfo bool
		jsonOutput      bool
		outputFormat    string
		quietMode       bool
		silentMode      bool
		logLevel        string
		verboseCount    int
	)

	rootCmd := &cobra.Command{
		Use:           "craftpack",
		Short:         "Standardized Linux packaging factory for the SDP",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.SetHelpTemplate(fmt.Sprintf(rootHelpTemplateFormat, cli.CleanVersion(cli.Version)))

	// Global / Persistent Flags
	rootCmd.PersistentFlags().BoolVarP(&showVersion, "version", "V", false, "Display single-line version of the Craftpack utility")
	rootCmd.PersistentFlags().BoolVar(&showVersionInfo, "version-info", false, "Display detailed build, compiler, and environment metadata")
	rootCmd.PersistentFlags().CountVarP(&verboseCount, "verbose", "v", "Increase diagnostic logging verbosity (-v: DEBUG, -vv: TRACE)")
	rootCmd.PersistentFlags().BoolVarP(&quietMode, "quiet", "q", false, "Quiet mode (suppresses all diagnostic outputs, showing only errors)")
	rootCmd.PersistentFlags().BoolVar(&silentMode, "silent", false, "Quiet mode (suppresses all diagnostic outputs, showing only errors)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "Explicitly override and set the logging verbosity level")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output results in machine-readable JSON format")
	rootCmd.PersistentFlags().StringVar(&outputFormat, "output", "", "Output format (e.g. json)")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		isJSON := jsonOutput || strings.EqualFold(outputFormat, "json")

		if showVersion {
			if verboseCount > 0 {
				if err := cli.PrintVersionInfo(cmd.OutOrStdout(), isJSON); err != nil {
					return cli.NewValidationError("failed printing version info: %w", err)
				}
				return errSilentSuccess
			}
			cli.PrintVersion(cmd.OutOrStdout())
			return errSilentSuccess
		}

		if showVersionInfo {
			if err := cli.PrintVersionInfo(cmd.OutOrStdout(), isJSON); err != nil {
				return cli.NewValidationError("failed printing version info: %w", err)
			}
			return errSilentSuccess
		}

		return nil
	}

	// Attach subcommands
	rootCmd.AddCommand(newBuildCommand(&jsonOutput, &outputFormat))
	rootCmd.AddCommand(newValidateCommand(&jsonOutput, &outputFormat))

	return rootCmd
}

// ExecuteContext runs the CLI with the provided context and argument slice,
// defaulting to os.Stdout and os.Stderr.
func ExecuteContext(ctx context.Context, args []string) int {
	return ExecuteContextWithStreams(ctx, args, os.Stdout, os.Stderr)
}

// ExecuteContextWithStreams coordinates log level resolution, logger initialization,
// argument evaluation, and exit code dispatch using the provided streams.
func ExecuteContextWithStreams(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	// 1. Resolve log level using Last-Flag-Wins priority
	level, err := cli.ResolveLogLevel(args, os.Getenv("CRAFTPACK_LOG_LEVEL"))
	if err != nil {
		fmt.Fprintf(stderr, "craftpack: %v\n", err)
		return cli.ExitUsage
	}

	// 2. Configure global logger routed to stderr
	color := cli.ColorEnabled(stderr)
	cli.SetupLogger(stderr, level, color)

	// 3. Instantiate root command
	rootCmd := NewRootCommand()
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)

	// 4. Execute command
	execErr := rootCmd.ExecuteContext(ctx)
	if execErr == nil {
		return cli.ExitSuccess
	}
	if errors.Is(execErr, errSilentSuccess) {
		return cli.ExitSuccess
	}
	if errors.Is(execErr, context.Canceled) || ctx.Err() == context.Canceled {
		return cli.ExitTerminated
	}

	code := cli.DetermineExitCode(execErr)
	var coder cli.ExitCoder
	if !errors.As(execErr, &coder) {
		// Cobra-level syntax or parsing errors map to ExitUsage (2)
		code = cli.ExitUsage
	}

	fmt.Fprintf(stderr, "craftpack: %v\n", execErr)
	return code
}
