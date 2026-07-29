package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultAPIVersion is the Meta Graph API version segment used when Config
// does not specify one. Meta ships a new major version roughly every few
// months and keeps each live for about two years, so this is a fallback, not a
// promise: Config.APIVersion (WHATSAPP_API_VERSION) is how an operator bumps
// it without a code change.
const defaultAPIVersion = "v25.0"

// defaultBaseURL is the Meta Graph API host. Only ever overridden in tests,
// which is why Config.BaseURL has no corresponding environment variable.
const defaultBaseURL = "https://graph.facebook.com"

// Config configures Client.
type Config struct {
	// AccessToken is the bearer token for the WhatsApp Business phone number
	// (WHATSAPP_ACCESS_TOKEN).
	AccessToken string
	// PhoneNumberID is the Meta-assigned ID of the sending WhatsApp Business
	// phone number (WHATSAPP_PHONE_NUMBER_ID).
	PhoneNumberID string
	// TemplateName is the pre-approved AUTHENTICATION-category template used to
	// deliver the code (WHATSAPP_TEMPLATE_NAME). Meta does not allow freeform
	// text for a business-initiated message to a user who has not messaged first,
	// which an OTP send always is.
	TemplateName string
	// TemplateLanguage is the template's language code, e.g. "en_US"
	// (WHATSAPP_TEMPLATE_LANGUAGE). VerificationSender.SendCode receives only a
	// phone and a code, not the user's preferred language, so every send
	// currently uses this one fixed language regardless of the account's
	// PreferredLanguage. Changing that would mean widening the VerificationSender
	// port, which is out of scope here.
	TemplateLanguage string
	// APIVersion is the Graph API version segment, e.g. "v25.0"
	// (WHATSAPP_API_VERSION). Defaults to defaultAPIVersion when empty.
	APIVersion string
	// BaseURL overrides the Graph API host. Empty means defaultBaseURL; tests
	// point this at an httptest.Server instead of the real graph.facebook.com.
	BaseURL string
}

// Client is the real auth.VerificationSender adapter: it sends an OTP as a
// Meta WhatsApp Cloud API AUTHENTICATION-category template message, the only
// kind of business-initiated message Meta permits to a user who has not
// messaged first.
//
// It is structured to Meta's documented WhatsApp Cloud API contract (POST
// {base}/{version}/{phone-number-id}/messages, a template message with a body
// parameter and a copy-code button parameter both carrying the OTP, per Meta's
// Authentication Templates documentation) but has not been exercised against a
// live send: doing so needs a real Meta Business/WhatsApp Cloud API access
// token, phone number ID, and an approved AUTHENTICATION template, none
// available in this environment (deferred to ops, mirroring the Google
// identity verifier adapter).
type Client struct {
	accessToken      string
	phoneNumberID    string
	templateName     string
	templateLanguage string
	apiVersion       string
	baseURL          string

	httpClient *http.Client
}

// NewClient builds the adapter from cfg. It does not validate cfg — an empty
// AccessToken or PhoneNumberID simply makes every SendCode fail; the
// composition root decides whether to construct a Client at all based on
// whether real credentials are configured.
func NewClient(cfg Config) *Client {
	apiVersion := cfg.APIVersion
	if apiVersion == "" {
		apiVersion = defaultAPIVersion
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		accessToken:      cfg.AccessToken,
		phoneNumberID:    cfg.PhoneNumberID,
		templateName:     cfg.TemplateName,
		templateLanguage: cfg.TemplateLanguage,
		apiVersion:       apiVersion,
		baseURL:          baseURL,
		httpClient:       &http.Client{Timeout: 10 * time.Second},
	}
}

// sendTemplateRequest is the Cloud API request body for a template message
// (Meta's Messages reference + Authentication Templates documentation).
type sendTemplateRequest struct {
	MessagingProduct string       `json:"messaging_product"`
	To               string       `json:"to"`
	Type             string       `json:"type"`
	Template         templateBody `json:"template"`
}

type templateBody struct {
	Name       string              `json:"name"`
	Language   templateLang        `json:"language"`
	Components []templateComponent `json:"components"`
}

type templateLang struct {
	Code string `json:"code"`
}

// templateComponent covers both components an authentication template send
// needs: the body parameter that renders the code into the message text, and
// the button parameter that fills the copy-code button's payload — Meta
// requires the same code in both (Authentication Templates documentation).
type templateComponent struct {
	Type       string              `json:"type"`
	SubType    string              `json:"sub_type,omitempty"`
	Index      string              `json:"index,omitempty"`
	Parameters []templateParameter `json:"parameters"`
}

type templateParameter struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// SendCode implements auth.VerificationSender structurally (this package
// intentionally does not import internal/auth — see package doc).
func (c *Client) SendCode(ctx context.Context, phone, code string) error {
	if c.accessToken == "" || c.phoneNumberID == "" {
		return fmt.Errorf("whatsapp: client not configured: missing access token or phone number id")
	}

	reqBody := sendTemplateRequest{
		MessagingProduct: "whatsapp",
		// Meta's own phone-number formatting guidance recommends keeping the leading
		// "+" and country code on "to" (omitting it risks the business's own country
		// code being prepended instead, misrouting the message) — this codebase's
		// phones are already normalized to "+20XXXXXXXXXX", so it is passed through
		// unchanged.
		To:   phone,
		Type: "template",
		Template: templateBody{
			Name:     c.templateName,
			Language: templateLang{Code: c.templateLanguage},
			Components: []templateComponent{
				{
					Type:       "body",
					Parameters: []templateParameter{{Type: "text", Text: code}},
				},
				{
					Type:       "button",
					SubType:    "url",
					Index:      "0",
					Parameters: []templateParameter{{Type: "text", Text: code}},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/%s/%s/messages", c.baseURL, c.apiVersion, c.phoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("whatsapp: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("whatsapp: send failed: %w", parseErrorResponse(resp))
	}
	return nil
}

// metaErrorResponse is Meta's documented error body shape on a non-2xx
// response: {"error": {"message", "type", "code", "error_data",
// "fbtrace_id"}}.
type metaErrorResponse struct {
	Error struct {
		Message   string `json:"message"`
		Type      string `json:"type"`
		Code      int    `json:"code"`
		ErrorData struct {
			Details string `json:"details"`
		} `json:"error_data"`
		FBTraceID string `json:"fbtrace_id"`
	} `json:"error"`
}

// parseErrorResponse extracts a useful error from a non-2xx Cloud API
// response, falling back to the raw status and body when it isn't Meta's
// documented error shape.
func parseErrorResponse(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	var parsed metaErrorResponse
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Error.Message == "" {
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	msg := fmt.Sprintf("status %d: meta error %d (%s): %s", resp.StatusCode, parsed.Error.Code, parsed.Error.Type, parsed.Error.Message)
	if parsed.Error.ErrorData.Details != "" {
		msg += ": " + parsed.Error.ErrorData.Details
	}
	if parsed.Error.FBTraceID != "" {
		msg += " (fbtrace_id " + parsed.Error.FBTraceID + ")"
	}
	return fmt.Errorf("%s", msg)
}
