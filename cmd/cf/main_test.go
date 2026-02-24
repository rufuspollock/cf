package main

import (
	"errors"
	"strings"
	"testing"
)

func TestExplainZoneCreatePermissionError_APIEnv(t *testing.T) {
	t.Setenv("CF_API_TOKEN", "test-token")
	t.Setenv("CLOUDFLARE_API_TOKEN", "")

	origRunner := cmdRunner
	t.Cleanup(func() {
		cmdRunner = origRunner
	})
	cmdRunner = func(name string, args ...string) ([]byte, error) {
		t.Fatalf("cmdRunner should not be called in API token mode")
		return nil, nil
	}

	err := errors.New(`0: Requires permission "com.cloudflare.api.account.zone.create" to create zones for the selected account`)
	got := explainZoneCreatePermissionError(err)
	if got == nil {
		t.Fatalf("expected error")
	}
	msg := got.Error()
	if !strings.Contains(msg, "Auth mode detected: API token from environment") {
		t.Fatalf("expected API mode guidance, got: %s", msg)
	}
	if !strings.Contains(msg, "Use a token with zone-creation capability") {
		t.Fatalf("expected API next steps, got: %s", msg)
	}
}

func TestExplainZoneCreatePermissionError_Wrangler(t *testing.T) {
	t.Setenv("CF_API_TOKEN", "")
	t.Setenv("CLOUDFLARE_API_TOKEN", "")

	origRunner := cmdRunner
	t.Cleanup(func() {
		cmdRunner = origRunner
	})
	cmdRunner = func(name string, args ...string) ([]byte, error) {
		if name != "wrangler" || len(args) != 1 || args[0] != "whoami" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return []byte("You are logged in with account example-account"), nil
	}

	err := errors.New(`0: Requires permission "com.cloudflare.api.account.zone.create" to create zones for the selected account`)
	got := explainZoneCreatePermissionError(err)
	if got == nil {
		t.Fatalf("expected error")
	}
	msg := got.Error()
	if !strings.Contains(msg, "Auth mode detected: Wrangler token fallback") {
		t.Fatalf("expected Wrangler mode guidance, got: %s", msg)
	}
	if !strings.Contains(msg, "You are logged in with account example-account") {
		t.Fatalf("expected whoami output, got: %s", msg)
	}
}

func TestExplainZoneCreatePermissionError_UnrelatedError(t *testing.T) {
	err := errors.New("some other error")
	got := explainZoneCreatePermissionError(err)
	if got != err {
		t.Fatalf("expected original error to be returned")
	}
}
