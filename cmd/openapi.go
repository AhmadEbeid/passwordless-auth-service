package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/auth"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/httpserver"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
)

var openapiOutPath string

var openapiCmd = &cobra.Command{
	Use:   "openapi",
	Short: "Print the generated OpenAPI spec (YAML)",
	Long: "Builds the same route table as `serve` — with no database or external " +
		"connections — and prints the resulting OpenAPI document. Used to feed " +
		"client generators (orval, swagger_parser) without a running server.",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := observability.NewLogger("error")
		_, api := httpserver.NewRouter(logger, noopPinger{}, nil)

		// Registration only declares routes; it never invokes the service, so nil
		// ports are safe here (no DB/config needed to emit the spec).
		auth.Register(api, auth.NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil))

		out := os.Stdout
		if openapiOutPath != "" {
			f, err := os.Create(openapiOutPath) //nolint:gosec // operator-supplied CLI flag, not untrusted input
			if err != nil {
				return fmt.Errorf("openapi: create output: %w", err)
			}
			defer func() { _ = f.Close() }()
			out = f
		}
		yaml, err := api.OpenAPI().YAML()
		if err != nil {
			return fmt.Errorf("openapi: render yaml: %w", err)
		}
		_, err = out.Write(yaml)
		return err
	},
}

func init() {
	openapiCmd.Flags().StringVarP(&openapiOutPath, "out", "o", "", "write to this file instead of stdout")
	rootCmd.AddCommand(openapiCmd)
}

type noopPinger struct{}

func (noopPinger) Ping(context.Context) error { return nil }
