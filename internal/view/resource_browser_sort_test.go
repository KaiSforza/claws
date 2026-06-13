package view

import "testing"

func TestParseNumericUsesDeterministicSuffixMultipliers(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{input: "1 B", want: 1},
		{input: "1 KB", want: 1000},
		{input: "1 MB", want: 1000 * 1000},
		{input: "1 KiB", want: 1024},
		{input: "1 MiB", want: 1024 * 1024},
		{input: "50%", want: 50},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseNumeric(tt.input)
			if err != nil {
				t.Fatalf("parseNumeric(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("parseNumeric(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
