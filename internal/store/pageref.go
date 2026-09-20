package store

import (
	"strconv"
	"strings"
)

// PageRef identifies a page within a crawl, scoped to its Confluence host.
// Host is the lower-cased tenant host (e.g. "company1.atlassian.net").
type PageRef struct {
	Host string
	ID   int64
}

// PageKey returns the canonical metadata key for a page reference: "host/123".
//
// The key is host-qualified for every crawl, single-host included, so there is
// exactly one key form in metadata.json, the link graph, seed IDs, front matter,
// and the filenames derived from them. Numeric page IDs are only unique within a
// tenant, so a bare ID would collide across hosts and would let one tenant's
// page or attachment overwrite another's.
//
// host is always the lower-cased tenant host in a real crawl. An empty host
// (tests, or a caller with no host context) falls back to the bare numeric ID so
// the key stays usable rather than becoming "/123".
func PageKey(host string, id int64) string {
	if host == "" {
		return strconv.FormatInt(id, 10)
	}
	return host + "/" + strconv.FormatInt(id, 10)
}

// Key returns the canonical metadata key for the reference.
func (r PageRef) Key() string {
	return PageKey(r.Host, r.ID)
}

// FlattenPageKey converts a metadata page key into a single filename-safe path
// segment.
//
// Keys are normally host-qualified ("company1.atlassian.net/123") and must never
// be used as a path component: the key separator, the port colon, and the
// characters that are illegal in Windows filenames (plus control characters) are
// replaced with "_"/dropped. That yields "company1.atlassian.net_123", or
// "127.0.0.1_8080_123" for a host key carrying a port. A bare numeric key (only
// produced when a caller has no host context) is returned unchanged.
//
// Host identities are preserved on purpose: two tenants can share a numeric page
// ID and an attachment name, so collapsing a key to its bare page ID would let
// those files overwrite each other. Host names cannot contain "_" and page IDs
// are digits, so the replacements cannot make two distinct keys collide.
func FlattenPageKey(pageKey string) string {
	var b strings.Builder
	b.Grow(len(pageKey))
	for _, r := range pageKey {
		switch {
		case r < 0x20 || r == 0x7F:
			// Drop control characters entirely; they are invalid in filenames
			// and carry no host/page meaning.
			continue
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' ||
			r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
