package oscal

import (
	"regexp"
	"testing"
)

var uuidV5 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestUUIDIsDeterministicVersion5(t *testing.T) {
	a := UUID("catalog", "800-53")
	if a != UUID("catalog", "800-53") {
		t.Fatal("the same kind and key gave two UUIDs")
	}
	if !uuidV5.MatchString(a) {
		t.Fatalf("%s is not an RFC 9562 version 5 UUID", a)
	}
	if a == UUID("profile", "800-53") || a == UUID("catalog", "800-53b") {
		t.Fatal("a different kind or key must give a different UUID")
	}
	// The kind and key are separated, so "ab"+"c" and "a"+"bc" do not collide.
	if UUID("ab", "c") == UUID("a", "bc") {
		t.Fatal("kind and key run together")
	}
}

// Pinned: a change here means every exported identifier changed, which breaks
// any tool that tracks grc's OSCAL objects across exports.
func TestUUIDNamespaceIsPinned(t *testing.T) {
	if got, want := uuidNamespace.String(), "2e98bbc8-0256-4a99-a220-1f7b4c3f60a6"; got != want {
		t.Fatalf("namespace %s, want %s", got, want)
	}
	// Python's uuid.uuid5(namespace, "catalog\x00800-53") gives the same value.
	if got, want := UUID("catalog", "800-53"), "764494bc-c9d5-56cf-a95b-229ed1107418"; got != want {
		t.Fatalf("UUID(catalog, 800-53) = %s, want %s", got, want)
	}
}
