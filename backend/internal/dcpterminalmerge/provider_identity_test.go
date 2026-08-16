package dcpterminalmerge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicRepositoryIdentityForUsesTypedSupportedGHAPI(t *testing.T) {
	installTerminalMergeGHFixture(t, `
test "$#" -eq 4
test "$1" = api
test "$2" = --method
test "$3" = GET
test "$4" = repos/orenvlad-ai/wb-browser-extension
printf '%s\n' '{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":1335072844,"owner":{"id":237411244}}'
`)
	got, err := publicRepositoryIdentityFor(context.Background(), "orenvlad-ai/wb-browser-extension")
	if err != nil || got != "orenvlad-ai/wb-browser-extension|false|main|1335072844|237411244" {
		t.Fatalf("provider identity = %q, err=%v", got, err)
	}
}

func TestPublicRepositoryIdentityForFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		wantExact bool
		wantError bool
	}{
		{name: "private", response: `{"full_name":"orenvlad-ai/wb-browser-extension","private":true,"default_branch":"main","id":1335072844,"owner":{"id":237411244}}`},
		{name: "old URL redirect full name", response: `{"full_name":"orenvlad-ai/wb-price-extension","private":false,"default_branch":"main","id":1335072844,"owner":{"id":237411244}}`},
		{name: "renamed", response: `{"full_name":"orenvlad-ai/foreign","private":false,"default_branch":"main","id":1335072844,"owner":{"id":237411244}}`},
		{name: "wrong branch", response: `{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"master","id":1335072844,"owner":{"id":237411244}}`},
		{name: "wrong repository id", response: `{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":1,"owner":{"id":237411244}}`},
		{name: "wrong owner id", response: `{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":1335072844,"owner":{"id":1}}`},
		{name: "missing field", response: `{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":1335072844}`, wantError: true},
		{name: "null field", response: `{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":null,"owner":{"id":237411244}}`, wantError: true},
		{name: "wrong type", response: `{"full_name":"orenvlad-ai/wb-browser-extension","private":false,"default_branch":"main","id":"1335072844","owner":{"id":237411244}}`, wantError: true},
		{name: "malformed", response: `{`, wantError: true},
	}
	const exact = "orenvlad-ai/wb-browser-extension|false|main|1335072844|237411244"
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			installTerminalMergeGHFixture(t, "printf '%s\\n' '"+test.response+"'")
			got, err := publicRepositoryIdentityFor(context.Background(), "orenvlad-ai/wb-browser-extension")
			if test.wantError {
				if err == nil {
					t.Fatalf("incomplete identity was accepted: %q", got)
				}
				return
			}
			if err != nil || got == exact {
				t.Fatalf("inexact identity crossed the equality gate: %q, err=%v", got, err)
			}
		})
	}
}

func TestPublicRepositoryIdentityForRejectsCommandFailure(t *testing.T) {
	installTerminalMergeGHFixture(t, "exit 1")
	if got, err := publicRepositoryIdentityFor(context.Background(), "orenvlad-ai/wb-browser-extension"); err == nil {
		t.Fatalf("provider command failure was accepted: %q", got)
	}
}

func installTerminalMergeGHFixture(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}
