// Package config loads and validates process configuration from the
// environment.
package config

import (
	"fmt"
	"os"
	"strings"
)

// EnvProduction is the APP_ENV value that turns optional-secret fallbacks into
// startup failures.
const EnvProduction = "production"

// Config is the validated application configuration.
type Config struct {
	DatabaseURL string
	HTTPPort    string
	LogLevel    string
	Env         string

	// AdminAPIKey/AdminID gate /admin/*. A blank AdminAPIKey rejects every admin
	// request rather than allowing one.
	AdminAPIKey string
	AdminID     string

	// AdminAllowedOrigins is the comma-separated list of origins the admin
	// dashboard (a browser app, unlike mobile's native client) is served from —
	// required for the browser to allow the cross-origin request at all. Empty by
	// default (no CORS headers sent, fail closed like AdminAPIKey).
	AdminAllowedOrigins []string

	// GoogleClientID is the OAuth client ID Google ID tokens must be issued for.
	// Blank until a real Google OAuth client is provisioned.
	GoogleClientID string

	// GoogleLinkSecret signs the short-lived pending-Google-link token that
	// proves a Google identity was verified before RequestVerification links it
	// to a new account. Required in production. Blank in development makes the
	// server generate a per-process secret, which is fine for one instance but
	// breaks any signup whose next request lands on another replica or arrives
	// after a restart.
	GoogleLinkSecret string

	// WhatsAppAccessToken and WhatsAppPhoneNumberID are the Meta WhatsApp Cloud
	// API credentials for the OTP-sending adapter. Both blank until a real Meta
	// Business/WhatsApp Cloud API app is provisioned; cmd/serve.go falls back to
	// a debug-log stub sender when either is unset.
	WhatsAppAccessToken   string
	WhatsAppPhoneNumberID string

	// WhatsAppTemplateName is the pre-approved AUTHENTICATION-category template
	// used to deliver the code — Meta does not allow freeform text for a
	// business-initiated message to a user who has not messaged first.
	WhatsAppTemplateName string
	// WhatsAppTemplateLanguage is that template's language code. The
	// VerificationSender port takes only a phone and a code, not the user's
	// preferred language, so every send currently uses this one fixed language
	// regardless of the account's PreferredLanguage.
	WhatsAppTemplateLanguage string
	// WhatsAppAPIVersion is the Meta Graph API version segment, e.g. "v25.0".
	WhatsAppAPIVersion string
}

// Load reads configuration from the environment and validates required fields.
// It fails fast: a missing required value returns an error rather than a zero
// value.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		HTTPPort:                 getenvDefault("HTTP_PORT", "8080"),
		LogLevel:                 getenvDefault("LOG_LEVEL", "info"),
		Env:                      getenvDefault("APP_ENV", "development"),
		AdminAPIKey:              os.Getenv("ADMIN_API_KEY"),
		AdminID:                  getenvDefault("ADMIN_ID", "admin"),
		AdminAllowedOrigins:      splitCSV(os.Getenv("ADMIN_ALLOWED_ORIGINS")),
		GoogleClientID:           os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleLinkSecret:         os.Getenv("GOOGLE_LINK_SECRET"),
		WhatsAppAccessToken:      os.Getenv("WHATSAPP_ACCESS_TOKEN"),
		WhatsAppPhoneNumberID:    os.Getenv("WHATSAPP_PHONE_NUMBER_ID"),
		WhatsAppTemplateName:     getenvDefault("WHATSAPP_TEMPLATE_NAME", "otp_verification"),
		WhatsAppTemplateLanguage: getenvDefault("WHATSAPP_TEMPLATE_LANGUAGE", "en_US"),
		WhatsAppAPIVersion:       getenvDefault("WHATSAPP_API_VERSION", "v25.0"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}
	// A blank link secret makes the server improvise a per-process one, which
	// silently breaks Google signup across replicas and restarts. Tolerable in
	// development, never in production.
	if cfg.IsProduction() && cfg.GoogleLinkSecret == "" {
		return Config{}, fmt.Errorf("config: GOOGLE_LINK_SECRET is required when APP_ENV=production")
	}
	return cfg, nil
}

// IsProduction reports whether this process is configured as a production
// deployment, which is what turns the optional-secret warnings above into
// hard failures.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
