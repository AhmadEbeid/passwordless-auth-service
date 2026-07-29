package auth_test

import (
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/auth"
)

// TestOpenAPIEmitsTypedErrorModel proves the generated OpenAPI error responses
// carry the feature's typed error fields
// (code/locked_until/attempts_remaining) and at least one typed code string,
// rather than Huma's default ErrorModel (title/status/detail). Admin (orval)
// and mobile (swagger_parser) generate clients from this spec, so the typed
// shape must appear in the schema The auth package's huma.NewError hook plus
// the per-operation Errors lists are what put it there.
func TestOpenAPIEmitsTypedErrorModel(t *testing.T) {
	_, api := humatest.New(t)
	auth.Register(api, auth.NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil))

	spec, err := api.OpenAPI().YAML()
	if err != nil {
		t.Fatalf("marshal OpenAPI to YAML: %v", err)
	}
	doc := string(spec)

	for _, want := range []string{"locked_until", "attempts_remaining", "account_exists"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("generated OpenAPI is missing %q; error responses must serialize the typed apiError model, not Huma's default ErrorModel", want)
		}
	}
}
