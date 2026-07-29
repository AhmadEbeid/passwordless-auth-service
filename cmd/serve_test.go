package cmd_test

import (
	"context"
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/cmd"
)

func TestRunServe_ConfigError(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := cmd.RunServe(context.Background()); err == nil {
		t.Fatal("expected config error when DATABASE_URL is unset")
	}
}
