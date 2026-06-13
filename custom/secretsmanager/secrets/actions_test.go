package secrets

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestFormatSecretValueSanitizesTerminalControls(t *testing.T) {
	secret := "value\nline2\x1b[2Jstill-here"
	got := formatSecretValue("secret\x1b[31m-id", &secret, nil)
	if strings.Contains(got, "\x1b") {
		t.Fatalf("formatted secret contains escape sequence: %q", got)
	}
	if !strings.Contains(got, "secret-id") || !strings.Contains(got, "value\nline2still-here") {
		t.Fatalf("formatted secret lost expected content: %q", got)
	}
}

func TestFormatSecretValueEncodesBinarySecret(t *testing.T) {
	binary := []byte{0, 1, 2, 3}
	got := formatSecretValue("binary-secret", nil, binary)
	want := base64.StdEncoding.EncodeToString(binary)
	if !strings.Contains(got, want) {
		t.Fatalf("formatted secret = %q, want base64 payload %q", got, want)
	}
}
