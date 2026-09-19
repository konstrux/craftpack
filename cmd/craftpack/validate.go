// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"

	"craftpack/pkg/cli"
	"craftpack/pkg/spec"

	"github.com/spf13/cobra"
)

const validateHelpTemplate = `craftpack validate - Validate the syntax, schema, and paths of a craftpack.yml specification

USAGE:
  craftpack validate --spec <path> [options]

OPTIONS:
  -s, --spec <path>  Filepath to declarative configuration file [default: craftpack.yml]
      --strict       Treat linter or schema warnings as hard errors (exit 1)

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
  craftpack validate --spec craftpack.yml
  craftpack validate --spec config/craftpack.yml --strict
`

func newValidateCommand(globalJSON *bool, globalOutput *string) *cobra.Command {
	var (
		specPath string
		strict   bool
	)

	cmd := &cobra.Command{
		Use:           "validate",
		Short:         "Validate the syntax, schema, and paths of a craftpack.yml specification",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cleanSpecPath := strings.TrimSpace(specPath)
			if cleanSpecPath == "" {
				cleanSpecPath = "craftpack.yml"
			}
			absSpecPath, err := filepath.Abs(cleanSpecPath)
			if err != nil {
				return cli.NewValidationError("failed to resolve specification path %q: %v", cleanSpecPath, err)
			}
			workspaceDir := filepath.Dir(absSpecPath)

			slog.Debug("Validating specification", "spec", absSpecPath, "workspace", workspaceDir, "strict", strict)

			parseRes, err := spec.ParseFile(absSpecPath, spec.ParseOptions{
				WorkspaceDir:   workspaceDir,
				CheckWorkspace: true,
				Strict:         strict,
			})
			if err != nil {
				return cli.NewValidationError("validation failed: %v", err)
			}

			// Report non-fatal schema warnings
			for _, w := range parseRes.Warnings {
				slog.Warn(w)
			}

			slog.Info("Specification is valid", "spec", cleanSpecPath, "package", parseRes.Config.Name)

			// Machine-readable validation payload
			isJSON := (globalJSON != nil && *globalJSON) || (globalOutput != nil && strings.EqualFold(*globalOutput, "json"))
			if isJSON {
				valResult := struct {
					Valid    bool     `json:"valid"`
					Spec     string   `json:"spec"`
					Package  string   `json:"package"`
					Warnings []string `json:"warnings"`
				}{
					Valid:    true,
					Spec:     cleanSpecPath,
					Package:  parseRes.Config.Name,
					Warnings: parseRes.Warnings,
				}
				if valResult.Warnings == nil {
					valResult.Warnings = make([]string, 0)
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(valResult); err != nil {
					return cli.NewValidationError("failed to encode result JSON: %w", err)
				}
			}

			return nil
		},
	}

	cmd.SetHelpTemplate(validateHelpTemplate)

	cmd.Flags().StringVarP(&specPath, "spec", "s", "craftpack.yml", "Filepath to declarative configuration file")
	cmd.Flags().BoolVar(&strict, "strict", false, "Treat linter or schema warnings as hard errors (exit 1)")

	return cmd
}
