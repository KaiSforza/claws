package functions

import (
	"strings"
	"testing"
)

func TestLambdaPayloadPreviewRedactsSensitiveOutput(t *testing.T) {
	payload := []byte(`{"password":"super-secret","token":"secret-token","ok":true}`)
	got := lambdaPayloadPreview(payload)

	for _, leaked := range []string{"super-secret", "secret-token"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("lambdaPayloadPreview leaked %q in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("lambdaPayloadPreview = %q, want redaction marker", got)
	}
}

func TestLambdaPayloadPreviewRedactsPlainTextOutput(t *testing.T) {
	got := lambdaPayloadPreview([]byte("token=secret-token"))
	if strings.Contains(got, "secret-token") {
		t.Fatalf("lambdaPayloadPreview leaked plaintext secret in %q", got)
	}
}

func TestLambdaPayloadPreviewTruncatesPlainTextOutput(t *testing.T) {
	got := lambdaPayloadPreview([]byte(strings.Repeat("a", 120)))
	if len(got) != 103 {
		t.Fatalf("lambdaPayloadPreview length = %d, want 103", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("lambdaPayloadPreview = %q, want ellipsis suffix", got)
	}
}
