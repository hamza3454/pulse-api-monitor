package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestIsValidID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"3f2504e0-4f89-41d3-9a0c-0305e82c3301", true},
		{"3F2504E0-4F89-41D3-9A0C-0305E82C3301", true},
		{"", false},
		{"not-a-uuid", false},
		{"3f2504e0-4f89-41d3-9a0c-0305e82c330", false},   // too short
		{"3f2504e0-4f89-41d3-9a0c-0305e82c33011", false}, // too long
		{"3f2504e0-4f89-41d3-9a0c-0305e82c33zz", false},  // non-hex
		{"3f2504e04f8941d39a0c0305e82c3301", false},      // no dashes
		{"3f2504e0-4f89-41d3-9a0c-0305e82c3301\n", false},
	}
	for _, tt := range tests {
		if got := IsValidID(tt.id); got != tt.want {
			t.Errorf("IsValidID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestValidateReturnsValidationError(t *testing.T) {
	m := validMonitor()
	m.Name = ""
	m.ExpectedStatus = 0

	err := m.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate() = %v, want *ValidationError", err)
	}
	if len(ve.Problems) != 2 {
		t.Errorf("Problems = %v, want 2 entries", ve.Problems)
	}
	if !strings.Contains(ve.Error(), "; ") {
		t.Errorf("Error() = %q, want problems joined with '; '", ve.Error())
	}
}

func TestValidateNilWhenValid(t *testing.T) {
	if err := validMonitor().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}
