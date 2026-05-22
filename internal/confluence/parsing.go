package confluence

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var pageIDFromPathPattern = regexp.MustCompile(`/pages/(\d+)`)

func parseConfluenceTime(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05.000Z", v); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", v); err == nil {
		return t
	}
	return time.Time{}
}

func parseSeedPageID(seed string) (int, error) {
	if seed == "" {
		return 0, fmt.Errorf("seed is empty")
	}

	if id, err := strconv.Atoi(seed); err == nil && id > 0 {
		return id, nil
	}

	u, err := url.Parse(seed)
	if err != nil {
		return 0, fmt.Errorf("invalid seed URL %q: %w", seed, err)
	}

	if q := u.Query().Get("pageId"); q != "" {
		id, err := strconv.Atoi(q)
		if err != nil || id <= 0 {
			return 0, fmt.Errorf("invalid pageId query value in seed %q", seed)
		}
		return id, nil
	}

	matches := pageIDFromPathPattern.FindStringSubmatch(u.Path)
	if len(matches) == 2 {
		id, err := strconv.Atoi(matches[1])
		if err != nil || id <= 0 {
			return 0, fmt.Errorf("invalid page ID in seed path %q", seed)
		}
		return id, nil
	}

	return 0, fmt.Errorf("could not extract page ID from seed %q", seed)
}
