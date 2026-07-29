package cmd

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/auth"
	authpg "github.com/AhmadEbeid/passwordless-auth-service/internal/auth/postgres"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/config"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/db"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/httpserver"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/observability"
	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/whatsapp"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunServe(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

// RunServe loads config and runs the HTTP server until an interrupt signal.
func RunServe(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := observability.NewLogger(cfg.LogLevel)
	tp, err := observability.NewTracerProvider(ctx, cfg)
	if err != nil {
		return fmt.Errorf("serve: tracer: %w", err)
	}
	defer func() { _ = tp.Shutdown(ctx) }()

	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	router, api := httpserver.NewRouter(logger, pool, cfg.AdminAllowedOrigins)

	// Auth feature composition: repositories over the pool, a real clock, and an
	// OTP sender. auth.Register mounts the operations on the Huma API.
	linkSecret, err := googleLinkSecret(cfg, logger)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	authRepos := authpg.New(pool)
	authService := auth.NewService(
		systemClock{},
		selectWhatsAppSender(cfg, logger),
		auth.NewGoogleIdentityVerifier(cfg.GoogleClientID),
		authRepos.Accounts,
		authRepos.Verifications,
		authRepos.Sessions,
		authRepos.AuditEvents,
		db.NewTxManager(pool),
		linkSecret,
	).WithAdmin(cfg.AdminAPIKey, cfg.AdminID)
	auth.Register(api, authService) //nolint:contextcheck // false positive: traces into SessionMiddleware, whose huma.Context (not context.Context) already threads ctx.Context() through correctly

	srv := httpserver.New(cfg, router)

	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("http server starting", "port", cfg.HTTPPort, "env", cfg.Env)
	return srv.Run(signalCtx)
}

// googleLinkSecret returns the configured pending-Google-link signing secret,
// or a per-process one when it is unset. config.Load already refuses to start a
// production process without it, so reaching the fallback means development —
// but it still warns, because the failure it causes is invisible from the
// outside: a signup whose next request lands on another replica, or arrives
// after a restart, fails with google_failed and nothing explains why.
func googleLinkSecret(cfg config.Config, logger *slog.Logger) ([]byte, error) {
	if cfg.GoogleLinkSecret != "" {
		return []byte(cfg.GoogleLinkSecret), nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate google link secret: %w", err)
	}
	logger.Warn("google link secret: none configured, generated one for this process only",
		"impact", "pending Google signups break across replicas and restarts",
		"fix", "set GOOGLE_LINK_SECRET")
	return secret, nil
}

// systemClock is the production auth.Clock backed by the wall clock.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// selectWhatsAppSender picks the real Meta Cloud API client when credentials
// are configured and the logging sender otherwise, announcing which so an
// operator can tell from the startup log whether codes are actually going out.
func selectWhatsAppSender(cfg config.Config, logger *slog.Logger) auth.VerificationSender {
	if cfg.WhatsAppAccessToken != "" && cfg.WhatsAppPhoneNumberID != "" {
		logger.Info("whatsapp sender: using the Meta Cloud API client")
		return whatsapp.NewClient(whatsapp.Config{
			AccessToken:      cfg.WhatsAppAccessToken,
			PhoneNumberID:    cfg.WhatsAppPhoneNumberID,
			TemplateName:     cfg.WhatsAppTemplateName,
			TemplateLanguage: cfg.WhatsAppTemplateLanguage,
			APIVersion:       cfg.WhatsAppAPIVersion,
		})
	}
	logger.Warn("whatsapp sender: no credentials configured, codes will be logged instead of sent",
		"missing", "WHATSAPP_ACCESS_TOKEN/WHATSAPP_PHONE_NUMBER_ID")
	return whatsapp.NewLoggingSender()
}
