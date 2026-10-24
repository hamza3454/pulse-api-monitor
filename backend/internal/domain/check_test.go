package domain

import "testing"

func TestNewIDIsValidV4UUID(t *testing.T) {
	id := NewID()
	if !IsValidID(id) {
		t.Fatalf("NewID() = %q, want a well-formed UUID", id)
	}
	if id[14] != '4' {
		t.Errorf("NewID() = %q, want version nibble 4", id)
	}
	if v := id[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Errorf("NewID() = %q, want RFC 4122 variant (8, 9, a or b), got %q", id, v)
	}
}

func TestNewIDIsUnique(t *testing.T) {
	seen := make(map[string]struct{}, 10000)
	for range 10000 {
		id := NewID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}
