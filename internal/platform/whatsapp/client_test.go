package whatsapp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/whatsapp"
)

func TestSendCode_Success(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotAuth   string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"messaging_product": "whatsapp",
			"contacts": [{"input": "+201234567890", "wa_id": "201234567890"}],
			"messages": [{"id": "wamid.abc123", "message_status": "accepted"}]
		}`))
	}))
	defer srv.Close()

	c := whatsapp.NewClient(whatsapp.Config{
		AccessToken:      "test-token",
		PhoneNumberID:    "1234567890",
		TemplateName:     "otp_verification",
		TemplateLanguage: "en_US",
		APIVersion:       "v25.0",
		BaseURL:          srv.URL,
	})

	if err := c.SendCode(context.Background(), "+201234567890", "483920"); err != nil {
		t.Fatalf("SendCode() error = %v, want nil", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "/v25.0/1234567890/messages"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-token")
	}

	if gotBody["messaging_product"] != "whatsapp" {
		t.Errorf("messaging_product = %v, want whatsapp", gotBody["messaging_product"])
	}
	if gotBody["to"] != "+201234567890" {
		t.Errorf("to = %v, want +201234567890 (leading + preserved per Meta's phone-number formatting guidance)", gotBody["to"])
	}
	if gotBody["type"] != "template" {
		t.Errorf("type = %v, want template", gotBody["type"])
	}

	tmpl, ok := gotBody["template"].(map[string]any)
	if !ok {
		t.Fatalf("template field missing or wrong type: %v", gotBody["template"])
	}
	if tmpl["name"] != "otp_verification" {
		t.Errorf("template.name = %v, want otp_verification", tmpl["name"])
	}
	lang, _ := tmpl["language"].(map[string]any)
	if lang["code"] != "en_US" {
		t.Errorf("template.language.code = %v, want en_US", lang["code"])
	}

	components, ok := tmpl["components"].([]any)
	if !ok || len(components) != 2 {
		t.Fatalf("template.components = %v, want 2 components", tmpl["components"])
	}

	body, _ := components[0].(map[string]any)
	if body["type"] != "body" {
		t.Errorf("components[0].type = %v, want body", body["type"])
	}
	bodyParams, _ := body["parameters"].([]any)
	if len(bodyParams) != 1 {
		t.Fatalf("components[0].parameters = %v, want 1 entry", body["parameters"])
	}
	bodyParam, _ := bodyParams[0].(map[string]any)
	if bodyParam["type"] != "text" || bodyParam["text"] != "483920" {
		t.Errorf("components[0].parameters[0] = %v, want text/483920", bodyParam)
	}

	button, _ := components[1].(map[string]any)
	if button["type"] != "button" || button["sub_type"] != "url" || button["index"] != "0" {
		t.Errorf("components[1] = %v, want button/url/index 0", button)
	}
	buttonParams, _ := button["parameters"].([]any)
	if len(buttonParams) != 1 {
		t.Fatalf("components[1].parameters = %v, want 1 entry", button["parameters"])
	}
	buttonParam, _ := buttonParams[0].(map[string]any)
	if buttonParam["type"] != "text" || buttonParam["text"] != "483920" {
		t.Errorf("components[1].parameters[0] = %v, want text/483920", buttonParam)
	}
}

func TestSendCode_MetaErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"error": {
				"message": "(#132001) Template name does not exist in the translation",
				"type": "OAuthException",
				"code": 132001,
				"error_data": {"messaging_product": "whatsapp", "details": "template not found"},
				"fbtrace_id": "Az8or2yhqkZfEZ-_4Qn_Bam"
			}
		}`))
	}))
	defer srv.Close()

	c := whatsapp.NewClient(whatsapp.Config{
		AccessToken:   "test-token",
		PhoneNumberID: "1234567890",
		TemplateName:  "does_not_exist",
		BaseURL:       srv.URL,
	})

	err := c.SendCode(context.Background(), "+201234567890", "483920")
	if err == nil {
		t.Fatal("SendCode() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "132001") {
		t.Errorf("error = %q, want it to mention meta error code 132001", err.Error())
	}
	if !strings.Contains(err.Error(), "translation") {
		t.Errorf("error = %q, want it to mention meta's error message", err.Error())
	}
}

func TestSendCode_NonJSONErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("upstream unavailable"))
	}))
	defer srv.Close()

	c := whatsapp.NewClient(whatsapp.Config{
		AccessToken:   "test-token",
		PhoneNumberID: "1234567890",
		BaseURL:       srv.URL,
	})

	err := c.SendCode(context.Background(), "+201234567890", "483920")
	if err == nil {
		t.Fatal("SendCode() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "upstream unavailable") {
		t.Errorf("error = %q, want it to mention status 503 and the raw body", err.Error())
	}
}

func TestSendCode_MissingCredentials(t *testing.T) {
	c := whatsapp.NewClient(whatsapp.Config{})

	err := c.SendCode(context.Background(), "+201234567890", "483920")
	if err == nil {
		t.Fatal("SendCode() error = nil, want non-nil when access token and phone number id are unset")
	}
}

func TestNewClient_Defaults(t *testing.T) {
	// Constructing with no APIVersion/BaseURL should not panic and should still
	// produce a usable client — verified indirectly via a real send against a
	// local server pointed at the default-derived URL shape by overriding BaseURL
	// only (APIVersion left blank to hit defaultAPIVersion).
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messaging_product":"whatsapp","contacts":[],"messages":[{"id":"wamid.x"}]}`))
	}))
	defer srv.Close()

	c := whatsapp.NewClient(whatsapp.Config{
		AccessToken:   "test-token",
		PhoneNumberID: "42",
		BaseURL:       srv.URL,
	})
	if err := c.SendCode(context.Background(), "+201234567890", "000000"); err != nil {
		t.Fatalf("SendCode() error = %v, want nil", err)
	}
	if want := "/v25.0/42/messages"; gotPath != want {
		t.Errorf("path = %q, want %q (default API version)", gotPath, want)
	}
}
