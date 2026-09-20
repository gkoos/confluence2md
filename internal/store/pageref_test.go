package store

import (
	"strings"
	"testing"
)

func TestFlattenPageKey_Matrix(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "bare numeric key with no host context is unchanged", in: "123", want: "123"},
		{name: "multi-host host-qualified key", in: "company1.atlassian.net/123", want: "company1.atlassian.net_123"},
		{name: "port-carrying host key", in: "127.0.0.1:8080/123", want: "127.0.0.1_8080_123"},
		{name: "windows separator", in: `company1.atlassian.net\123`, want: "company1.atlassian.net_123"},
		{name: "windows-reserved characters", in: `host*?"<>|/123`, want: "host" + strings.Repeat("_", 7) + "123"},
		{name: "control characters are dropped", in: "host\x00\x1f/123", want: "host_123"},
		{name: "empty input stays empty", in: "", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FlattenPageKey(tc.in); got != tc.want {
				t.Fatalf("FlattenPageKey(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFlattenPageKey_NeverLeaksSeparatorsOrIllegalCharacters(t *testing.T) {
	keys := []string{
		"123",
		PageKey("company1.atlassian.net", 123),
		PageKey("company2.atlassian.net", 123),
		PageKey("127.0.0.1:8080", 123),
		`host\123`,
		"host\x7f/123",
	}

	for _, key := range keys {
		got := FlattenPageKey(key)
		if strings.ContainsAny(got, `/\:*?"<>|`) {
			t.Fatalf("FlattenPageKey(%q) = %q: still contains a path separator or a filename-illegal character", key, got)
		}
		if strings.ContainsAny(got, "\x00\x1f\x7f") {
			t.Fatalf("FlattenPageKey(%q) = %q: still contains a control character", key, got)
		}
	}
}

func TestFlattenPageKey_DistinctHostsKeepHostIdentity(t *testing.T) {
	bare := FlattenPageKey(PageKey("", 123))
	first := FlattenPageKey(PageKey("company1.atlassian.net", 123))
	second := FlattenPageKey(PageKey("company2.atlassian.net", 123))

	if first != "company1.atlassian.net_123" {
		t.Fatalf("first host key = %q, want %q", first, "company1.atlassian.net_123")
	}
	if second != "company2.atlassian.net_123" {
		t.Fatalf("second host key = %q, want %q", second, "company2.atlassian.net_123")
	}
	if first == second || first == bare || second == bare {
		t.Fatalf("expected distinct flattened keys, got bare=%q first=%q second=%q", bare, first, second)
	}
}

func TestPageKey_AlwaysQualifiesWithHost(t *testing.T) {
	if got, want := PageKey("company1.atlassian.net", 123), "company1.atlassian.net/123"; got != want {
		t.Fatalf("PageKey = %q, want %q", got, want)
	}
	ref := PageRef{Host: "Company1.Atlassian.NET", ID: 123}
	if got, want := ref.Key(), "Company1.Atlassian.NET/123"; got != want {
		t.Fatalf("PageRef.Key = %q, want %q", got, want)
	}

	// A host-less reference is the only source of a bare key, and it still has to
	// produce something usable rather than a leading slash.
	if got, want := PageKey("", 123), "123"; got != want {
		t.Fatalf("PageKey with empty host = %q, want %q", got, want)
	}
}
