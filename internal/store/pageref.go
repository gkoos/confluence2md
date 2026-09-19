package store

import "strconv"

// PageRef identifies a page within a crawl, scoped to its Confluence host.
// Host is the lower-cased tenant host (e.g. "company1.atlassian.net").
type PageRef struct {
	Host string
	ID   int64
}

// PageKey returns the canonical metadata key for a page reference.
//
// Single-host crawls pass qualify=false and receive the legacy bare numeric
// key, keeping output byte-identical to previous releases. Multi-host crawls
// pass qualify=true and receive a host-qualified key so that distinct tenants
// sharing a numeric page ID never collide in the page map or link graph.
func PageKey(host string, id int64, qualify bool) string {
	if !qualify || host == "" {
		return strconv.FormatInt(id, 10)
	}
	return host + "/" + strconv.FormatInt(id, 10)
}

// Key returns the always host-qualified key used internally by the crawler's
// visited/results maps, where host is always populated.
func (r PageRef) Key() string {
	return PageKey(r.Host, r.ID, true)
}
