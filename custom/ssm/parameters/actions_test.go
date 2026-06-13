package parameters

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

func TestFormatParameterValueSanitizesTerminalControls(t *testing.T) {
	got := formatParameterValue("/prod/db\x1b[31m", "secret\nline2\x1b[2Jvalue")
	if strings.Contains(got, "\x1b") {
		t.Fatalf("formatted value contains escape sequence: %q", got)
	}
	if !strings.Contains(got, "/prod/db") || !strings.Contains(got, "secret\nline2value") {
		t.Fatalf("formatted value lost expected content: %q", got)
	}
}

func TestFormatParameterHistoryHandlesEmptyHistory(t *testing.T) {
	got := formatParameterHistory("/prod/db", nil)
	if !strings.Contains(got, "No history found") {
		t.Fatalf("formatted history = %q, want empty history message", got)
	}
}

func TestFormatParameterHistorySanitizesValues(t *testing.T) {
	value := "old\nline2\x1b[31mvalue"
	got := formatParameterHistory("/prod/db", []types.ParameterHistory{{Version: 7, Value: &value}})
	if strings.Contains(got, "\x1b") {
		t.Fatalf("formatted history contains escape sequence: %q", got)
	}
	if !strings.Contains(got, "Version 7") || !strings.Contains(got, "old\nline2value") {
		t.Fatalf("formatted history lost expected content: %q", got)
	}
}
