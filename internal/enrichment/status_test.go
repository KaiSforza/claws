package enrichment

import (
	"errors"
	"testing"

	"github.com/aws/smithy-go"
)

type mockAPIError struct {
	code    string
	message string
}

func (e *mockAPIError) Error() string                 { return e.message }
func (e *mockAPIError) ErrorCode() string             { return e.code }
func (e *mockAPIError) ErrorMessage() string          { return e.message }
func (e *mockAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

func TestFailureStatusPreservesCurrentBehavior(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Status
	}{
		{name: "access denied", err: &mockAPIError{code: "AccessDenied", message: "denied"}, want: AccessDenied},
		{name: "generic error", err: errors.New("boom"), want: FetchFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FailureStatus(tt.err); got != tt.want {
				t.Fatalf("FailureStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		codes []string
		want  Status
	}{
		{name: "access denied wins", err: &mockAPIError{code: "AccessDeniedException", message: "denied"}, codes: []string{"NoSuchTagSet"}, want: AccessDenied},
		{name: "not configured code", err: &mockAPIError{code: "NoSuchTagSet", message: "missing"}, codes: []string{"NoSuchTagSet"}, want: NotConfigured},
		{name: "generic error", err: errors.New("boom"), codes: []string{"NoSuchTagSet"}, want: FetchFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.err, tt.codes...); got != tt.want {
				t.Fatalf("ClassifyError() = %q, want %q", got, tt.want)
			}
		})
	}
}
