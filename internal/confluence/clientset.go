package confluence

// ClientSet routes requests to the correct per-host client in a multi-host
// crawl. Hosts are lower-cased tenant hosts (e.g. "company1.atlassian.net").
type ClientSet struct {
	byHost map[string]*Client
	order  []string
}

// NewClientSet returns an empty client set.
func NewClientSet() *ClientSet {
	return &ClientSet{byHost: make(map[string]*Client)}
}

// Add registers a client under its own host.
func (s *ClientSet) Add(c *Client) {
	host := c.Host()
	if host == "" {
		return
	}
	if _, ok := s.byHost[host]; !ok {
		s.order = append(s.order, host)
	}
	s.byHost[host] = c
}

// ForHost returns the client for the given host, or nil if unknown.
func (s *ClientSet) ForHost(host string) *Client {
	return s.byHost[host]
}

// Hosts returns the ordered list of registered hosts.
func (s *ClientSet) Hosts() []string {
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// AllowedHosts returns the ordered hosts, for link-scope matching.
func (s *ClientSet) AllowedHosts() []string {
	return s.Hosts()
}
