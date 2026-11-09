package oscal

import (
	"strings"

	"github.com/google/uuid"
)

// uuidNamespace is grc's fixed UUIDv5 namespace. It was generated once and must
// never change: every exported document and object UUID is derived from it, and
// a tool that tracks OSCAL objects across exports relies on them staying put.
var uuidNamespace = uuid.MustParse("2e98bbc8-0256-4a99-a220-1f7b4c3f60a6")

// UUID returns the deterministic UUID (version 5) of one object: kind names
// what it is ("catalog", "control-part", "mapping") and key identifies it
// stably within that kind ("800-53", "ac-1/grc-kri"). The same kind and key give
// the same UUID on every export and every installation, so two exports of
// unchanged data carry the same identifiers.
func UUID(kind, key string) string {
	return uuid.NewSHA1(uuidNamespace, []byte(strings.TrimSpace(kind)+"\x00"+strings.TrimSpace(key))).String()
}
