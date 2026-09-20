package crawl

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gkoos/confluence2md/internal/config"
	"github.com/gkoos/confluence2md/internal/confluence"
	"github.com/gkoos/confluence2md/internal/convert"
	"github.com/gkoos/confluence2md/internal/links"
	"github.com/gkoos/confluence2md/internal/store"
)

// CrawledPage represents a page after conversion and link extraction
type CrawledPage struct {
	ID                   int64
	Host                 string // lower-cased tenant host this page was fetched from
	Title                string
	Markdown             string
	Reused               bool
	Deleted              bool
	Comments             []confluence.CommentData
	CommentCount         int
	CommentFetchError    string
	RawADF               string // raw Confluence ADF JSON body
	CanonicalURL         string
	SpaceKey             string
	OutgoingLinks        []store.PageRef // host-scoped refs of all linked pages
	ExternalLinksSkipped int
	Version              int
	SourceURL            string
	CrawledAt            time.Time
	Depth                int
	FetchError           string // non-empty if fetch/convert failed

	Attachments          []confluence.AttachmentData
	AttachmentSignature  string
	AttachmentFetchError string

	// Temporal metadata
	CreatedAt      string
	LastModifiedAt string

	// Author metadata
	CreatedByID        string
	CreatedByName      string
	LastModifiedByID   string
	LastModifiedByName string

	// Hierarchy metadata
	ParentID *int64
}

type queueDropSample struct {
	PageID int64
	Depth  int
}

const maxQueueDropSamples = 20

// NodeHandlerResult is the mode-specific output produced per traversed node.
// The traversal engine only depends on OutgoingLinks, FetchError, and Deleted.
type NodeHandlerResult struct {
	Page                 *CrawledPage
	OutgoingLinks        []store.PageRef
	FetchError           string
	Deleted              bool
	Title                string
	ExternalLinksSkipped int
}

// CrawlNodeHandler processes a single traversed node.
type CrawlNodeHandler func(ctx context.Context, host string, pageID int64, depth int) *NodeHandlerResult

// CrawlSession manages the full BFS traversal
type CrawlSession struct {
	clients       *confluence.ClientSet
	config        *config.Config
	dryRun        bool
	maxDepth      int
	concurrency   int
	seedSpaceKey  string // resolved alpha space key for title lookups
	nodeHandler   CrawlNodeHandler
	previousPages map[string]store.PageRecord

	// BFS state
	queue   chan queueItem
	visited map[string]bool
	results map[string]*CrawledPage
	mu      sync.RWMutex

	// Concurrency control
	semaphore chan struct{}

	// Work tracking
	pendingWork sync.WaitGroup

	// Tracking
	totalFetched      int
	enqueueDrops      int
	enqueueDropSample []queueDropSample
}

type queueItem struct {
	host   string
	pageID int64
	depth  int
}

// NewCrawlSession creates a new BFS crawler session
func NewCrawlSession(clients *confluence.ClientSet, cfg *config.Config, seedSpaceKey string) *CrawlSession {
	cs := &CrawlSession{
		clients:      clients,
		config:       cfg,
		maxDepth:     cfg.Crawl.MaxDepth,
		concurrency:  cfg.Crawl.Concurrency,
		seedSpaceKey: seedSpaceKey,

		queue:     make(chan queueItem, cfg.Crawl.QueueSize),
		visited:   make(map[string]bool),
		results:   make(map[string]*CrawledPage),
		semaphore: make(chan struct{}),
	}

	// Default to full mode behavior; updates mode can override this callback.
	cs.nodeHandler = cs.processFullNode

	return cs
}

// SetNodeHandler overrides per-node processing while keeping traversal behavior identical.
func (cs *CrawlSession) SetNodeHandler(handler CrawlNodeHandler) error {
	if handler == nil {
		return fmt.Errorf("node handler cannot be nil")
	}
	cs.nodeHandler = handler
	return nil
}

// SetDryRun enables or disables dry-run traversal behavior.
func (cs *CrawlSession) SetDryRun(dryRun bool) {
	cs.dryRun = dryRun
}

// EnableUpdatesMode switches traversal to updates-mode node handling.
func (cs *CrawlSession) EnableUpdatesMode(previousPages map[string]store.PageRecord) {
	cs.previousPages = previousPages
	cs.nodeHandler = cs.processUpdatesNode
}

// Run executes the full BFS crawl starting from seed pages
func (cs *CrawlSession) Run(ctx context.Context, seeds []store.PageRef) (map[string]*CrawledPage, error) {
	// Initialize semaphore with concurrency limit
	cs.semaphore = make(chan struct{}, cs.concurrency)

	// Start worker goroutines
	var workerWg sync.WaitGroup
	for i := 0; i < cs.concurrency; i++ {
		workerWg.Add(1)
		go cs.worker(ctx, &workerWg)
	}

	// Enqueue seed pages at depth 0
	for _, seed := range seeds {
		key := seed.Key()
		cs.mu.Lock()
		if !cs.visited[key] {
			cs.visited[key] = true
			cs.pendingWork.Add(1)
			cs.queue <- queueItem{host: seed.Host, pageID: seed.ID, depth: 0}
		}
		cs.mu.Unlock()
	}

	// Wait for all pending work to complete
	cs.pendingWork.Wait()

	// Close queue to signal workers to exit
	close(cs.queue)
	workerWg.Wait()

	// If the context was cancelled or timed out, report that instead of success.
	if err := ctx.Err(); err != nil {
		return cs.results, err
	}

	if cs.enqueueDrops > 0 {
		return cs.results, fmt.Errorf("crawl queue saturated: dropped %d discovered page(s); sample=%s", cs.enqueueDrops, cs.queueDropSampleSummary())
	}

	return cs.results, nil
}

func (cs *CrawlSession) queueDropSampleSummary() string {
	if len(cs.enqueueDropSample) == 0 {
		return "none"
	}

	parts := make([]string, 0, len(cs.enqueueDropSample))
	for _, sample := range cs.enqueueDropSample {
		parts = append(parts, fmt.Sprintf("%d@d%d", sample.PageID, sample.Depth))
	}
	return strings.Join(parts, ",")
}

// worker processes items from the queue
func (cs *CrawlSession) worker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for item := range cs.queue {
		// Check for context cancellation before processing
		if ctx.Err() != nil {
			cs.pendingWork.Done()
			continue
		}

		// Check depth limit
		if item.depth > cs.maxDepth {
			cs.pendingWork.Done()
			continue
		}

		// Acquire semaphore slot with context awareness
		select {
		case cs.semaphore <- struct{}{}:
			// Acquired successfully
		case <-ctx.Done():
			cs.pendingWork.Done()
			continue
		}

		// Process node via mode-specific callback.
		result := cs.nodeHandler(ctx, item.host, item.pageID, item.depth)
		if result == nil {
			result = &NodeHandlerResult{FetchError: "node handler returned nil result"}
		}
		if result.Deleted {
			if result.Page == nil {
				result.Page = &CrawledPage{
					ID:        item.pageID,
					Host:      item.host,
					Deleted:   true,
					CrawledAt: time.Now(),
					Depth:     item.depth,
				}
			} else {
				result.Page.Deleted = true
			}
		}

		title := result.Title
		if title == "" && result.Page != nil {
			title = result.Page.Title
		}

		key := store.PageKey(item.host, item.pageID)

		cs.mu.Lock()
		if result.Page != nil {
			cs.results[key] = result.Page
		}
		cs.totalFetched++
		fetched := cs.totalFetched
		visited := len(cs.visited)
		cs.mu.Unlock()

		// Release semaphore BEFORE enqueuing children — otherwise all workers can
		// deadlock holding semaphore slots while blocking on a full queue channel.
		<-cs.semaphore

		childCount := 0
		if !result.Deleted && result.FetchError == "" && item.depth < cs.maxDepth {
			cs.enqueueChildren(item.depth, result.OutgoingLinks)
			childCount = len(result.OutgoingLinks)
		}

		depthPrefix := fmt.Sprintf("D%d", item.depth)
		if result.FetchError != "" {
			fmt.Printf("  [%s] ERR  %d — %s: %s\n", depthPrefix, item.pageID, title, result.FetchError)
		} else if result.Deleted {
			fmt.Printf("  [%s] DEL  %d — %s\n", depthPrefix, item.pageID, title)
		} else {
			fmt.Printf("  [%s] %3d/%-3d  %d — %s  (+%d links, ext-skip:%d, queue:%d)\n",
				depthPrefix, fetched, visited, item.pageID, title, childCount, result.ExternalLinksSkipped, len(cs.queue))
		}

		cs.pendingWork.Done()
	}
}

// processFullNode fetches, converts, and extracts links for full mode.
func (cs *CrawlSession) processFullNode(ctx context.Context, host string, pageID int64, depth int) *NodeHandlerResult {
	client := cs.clients.ForHost(host)
	if client == nil {
		page := &CrawledPage{
			ID:         pageID,
			Host:       host,
			Depth:      depth,
			CrawledAt:  time.Now(),
			FetchError: fmt.Sprintf("no client for host %q", host),
		}
		return &NodeHandlerResult{Page: page, FetchError: page.FetchError}
	}

	page := &CrawledPage{
		ID:        pageID,
		Host:      host,
		Depth:     depth,
		CrawledAt: time.Now(),
	}

	// Fetch page by ID
	fetchedPage, err := client.GetPageByID(ctx, pageID, cs.seedSpaceKey)
	if err != nil {
		if confluence.IsNotFound(err) {
			return deletedNodeResult(host, pageID, depth, "")
		}
		// A permission-denied fetch is reported and counted like any other
		// per-page failure and does not abort the crawl (FR-011). The
		// message is classified (missing scope vs rejected credential) so
		// the operator can tell the two apart without inspecting source or
		// network traces (FR-009, FR-010).
		page.FetchError = fmt.Sprintf("fetch failed: %s", confluence.DescribeAuthFailure(client.Mode(), fmt.Sprintf("page %d", pageID), err))
		return &NodeHandlerResult{Page: page, FetchError: page.FetchError}
	}
	if strings.EqualFold(strings.TrimSpace(fetchedPage.Status), "trashed") {
		return deletedNodeResult(host, pageID, depth, fetchedPage.Title)
	}

	page.Title = fetchedPage.Title
	page.SourceURL = fetchedPage.Links.Webui
	page.CanonicalURL = fetchedPage.Links.Webui
	page.Version = fetchedPage.Version.Number
	page.SpaceKey = fetchedPage.Space.Key
	page.RawADF = fetchedPage.Body.ADF.Value

	// Temporal metadata
	page.CreatedAt = fetchedPage.CreatedAt
	page.LastModifiedAt = fetchedPage.Version.CreatedAt

	// Author metadata
	page.CreatedByID = fetchedPage.AuthorID
	page.LastModifiedByID = fetchedPage.Version.AuthorID
	if page.CreatedByID != "" {
		page.CreatedByName = client.GetUserDisplayName(ctx, page.CreatedByID)
	}
	if page.LastModifiedByID != "" {
		page.LastModifiedByName = client.GetUserDisplayName(ctx, page.LastModifiedByID)
	}

	// Hierarchy metadata
	if fetchedPage.ParentID != "" {
		if parentID, err := strconv.ParseInt(fetchedPage.ParentID, 10, 64); err == nil {
			page.ParentID = &parentID
		}
	}

	// Fetch page comments (best-effort). Failure is non-fatal for page export.
	comments, err := client.GetPageComments(ctx, pageID)
	if err != nil {
		page.CommentFetchError = fmt.Sprintf("comments fetch failed: %v", err)
	} else {
		page.Comments = comments
		page.CommentCount = len(comments)
	}

	// Fetch attachment metadata (best-effort). Failure is non-fatal for page export.
	if cs.config.Attachments.Download {
		attachments, err := client.GetPageAttachments(ctx, pageID)
		if err != nil {
			page.AttachmentFetchError = fmt.Sprintf("attachments fetch failed: %v", err)
		} else {
			page.Attachments = attachments
			page.AttachmentSignature = confluence.ComputeAttachmentSignature(attachments)
		}
	}

	allowedHosts := cs.clients.AllowedHosts()

	// Extract outgoing page refs from ADF JSON
	page.OutgoingLinks, page.ExternalLinksSkipped = links.ExtractPageIDsFromADFWithStats(fetchedPage.Body.ADF.Value, host, allowedHosts)

	// Also extract links from comment bodies so pages referenced only in
	// comments are discovered and crawled.
	for _, comment := range page.Comments {
		commentRefs, _ := links.ExtractPageIDsFromADFWithStats(comment.Body, host, allowedHosts)
		page.OutgoingLinks = links.DedupPageRefs(append(page.OutgoingLinks, commentRefs...))
	}

	// If the page contains a "children" macro, or follow_children is enabled,
	// child pages are not represented as inline links in ADF — fetch them via
	// the API and add to outgoing links.
	if links.HasChildrenMacro(fetchedPage.Body.ADF.Value) || cs.config.Crawl.FollowChildren {
		childIDs, err := client.GetPageChildIDs(ctx, pageID)
		if err != nil {
			// Non-fatal: log but continue with whatever inline links we already have.
			fmt.Printf("  [D%d] WARN  %d — children macro fetch failed: %v\n", depth, pageID, err)
		} else {
			page.OutgoingLinks = links.DedupPageRefs(append(page.OutgoingLinks, refsForHost(host, childIDs)...))
		}
	}

	// If the page contains a "contentbylabel" macro, the listed pages are not
	// represented as inline links in ADF — execute the embedded CQL query and
	// add matching page IDs to the outgoing link set.
	//
	// In non-dry-run mode we also append a "Related pages" section to rendered
	// markdown so link rewriting can convert these URLs to local relative paths.
	for _, cql := range links.ExtractContentByLabelCQLs(fetchedPage.Body.ADF.Value) {
		cqlIDs, err := client.SearchPagesByCQL(ctx, cql)
		if err != nil {
			// Non-fatal: log but continue.
			fmt.Printf("  [D%d] WARN  %d — contentbylabel CQL search failed: %v\n", depth, pageID, err)
			continue
		}
		page.OutgoingLinks = links.DedupPageRefs(append(page.OutgoingLinks, refsForHost(host, cqlIDs)...))

		if cs.dryRun {
			continue
		}

		// Build a Related pages list and append to markdown. Use viewpage.action
		// URLs — the same format the link rewriter resolves to local paths.
		var relatedBuf strings.Builder
		relatedBuf.WriteString("\n\n## Related pages\n\n")
		siteBase := "https://" + host
		for _, id := range cqlIDs {
			title, err := client.GetPageTitleByID(ctx, int(id))
			if err != nil || title == "" {
				title = strconv.FormatInt(id, 10)
			}
			fmt.Fprintf(&relatedBuf, "- [%s](%s/wiki/pages/viewpage.action?pageId=%d)\n",
				title, siteBase, id)
		}
		page.Markdown += relatedBuf.String()
	}

	if !cs.dryRun {
		// Convert to Markdown only when a real run needs rendered output.
		markdown, err := convert.ToMarkdown(fetchedPage.Body.ADF.Value)
		if err != nil {
			page.FetchError = fmt.Sprintf("convert failed: %v", err)
			return &NodeHandlerResult{Page: page, FetchError: page.FetchError, Title: page.Title}
		}

		// Prepend page title as H1 only when it is not already present.
		if !hasLeadingTitleH1(markdown, page.Title) {
			markdown = fmt.Sprintf("# %s\n\n%s", page.Title, markdown)
		}

		page.Markdown = markdown + page.Markdown
	}

	return &NodeHandlerResult{
		Page:                 page,
		OutgoingLinks:        page.OutgoingLinks,
		FetchError:           page.FetchError,
		Title:                page.Title,
		ExternalLinksSkipped: page.ExternalLinksSkipped,
	}
}

// processUpdatesNode applies lightweight state classification before deciding whether
// to reuse prior metadata (clean) or run full processing (dirty).
func (cs *CrawlSession) processUpdatesNode(ctx context.Context, host string, pageID int64, depth int) *NodeHandlerResult {
	client := cs.clients.ForHost(host)
	if client == nil {
		return cs.processFullNode(ctx, host, pageID, depth)
	}

	pageIDStr := store.PageKey(host, pageID)
	previous, exists := cs.previousPages[pageIDStr]

	state, err := client.GetPageState(ctx, pageID, cs.config.Attachments.Download)
	if err != nil {
		if confluence.IsNotFound(err) {
			title := ""
			if exists {
				title = previous.Title
			}
			return deletedNodeResult(host, pageID, depth, title)
		}
		// Conservative fallback: unknown state is treated as dirty.
		return cs.processFullNode(ctx, host, pageID, depth)
	}
	if state != nil && strings.EqualFold(strings.TrimSpace(state.Status), "trashed") {
		title := state.Title
		if title == "" && exists {
			title = previous.Title
		}
		return deletedNodeResult(host, pageID, depth, title)
	}
	if state == nil || strings.TrimSpace(state.Title) == "" {
		// Conservative fallback for incomplete lightweight state.
		return cs.processFullNode(ctx, host, pageID, depth)
	}

	if !exists {
		return cs.processFullNode(ctx, host, pageID, depth)
	}

	if isDirtyComparedToPrevious(previous, state, cs.config.Attachments.Download) {
		return cs.processFullNode(ctx, host, pageID, depth)
	}

	outgoing := parseOutgoingLinkRefs(previous.OutgoingLinks, host)
	cleanPage := &CrawledPage{
		ID:                  pageID,
		Host:                host,
		Title:               previous.Title,
		Markdown:            previous.StorageFormat,
		Reused:              true,
		CanonicalURL:        previous.CanonicalURL,
		SpaceKey:            previous.SpaceKey,
		OutgoingLinks:       outgoing,
		Version:             previous.Version,
		SourceURL:           previous.SourceURL,
		CrawledAt:           previous.CrawledAt,
		Depth:               depth,
		AttachmentSignature: previous.AttachmentSignature,
		CreatedByID:         previous.CreatedByID,
		CreatedByName:       previous.CreatedByName,
		LastModifiedByID:    previous.LastModifiedByID,
		LastModifiedByName:  previous.LastModifiedByName,
		ParentID:            previous.ConfluenceParentID,
	}
	if !previous.CreatedAt.IsZero() {
		cleanPage.CreatedAt = previous.CreatedAt.Format(time.RFC3339)
	}
	if !previous.LastModifiedAt.IsZero() {
		cleanPage.LastModifiedAt = previous.LastModifiedAt.Format(time.RFC3339)
	}
	if cs.config.Attachments.Download && strings.TrimSpace(state.AttachmentSignature) != "" {
		cleanPage.AttachmentSignature = state.AttachmentSignature
	}

	return &NodeHandlerResult{
		Page:                 cleanPage,
		OutgoingLinks:        outgoing,
		Title:                cleanPage.Title,
		ExternalLinksSkipped: 0,
	}
}

func deletedNodeResult(host string, pageID int64, depth int, title string) *NodeHandlerResult {
	page := &CrawledPage{
		ID:        pageID,
		Host:      host,
		Title:     title,
		Deleted:   true,
		CrawledAt: time.Now(),
		Depth:     depth,
	}
	return &NodeHandlerResult{
		Page:    page,
		Deleted: true,
		Title:   title,
	}
}

func isDirtyComparedToPrevious(previous store.PageRecord, current *confluence.PageStateData, includeAttachments bool) bool {
	if current == nil {
		return true
	}
	if previous.Version != current.Version {
		return true
	}
	if previous.Title != current.Title {
		return true
	}
	if includeAttachments {
		if strings.TrimSpace(previous.AttachmentSignature) == "" {
			return true
		}
		if previous.AttachmentSignature != current.AttachmentSignature {
			return true
		}
	}
	return false
}

func refsForHost(host string, ids []int64) []store.PageRef {
	refs := make([]store.PageRef, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			refs = append(refs, store.PageRef{Host: host, ID: id})
		}
	}
	return refs
}

func parseOutgoingLinkRefs(ids []string, defaultHost string) []store.PageRef {
	out := make([]store.PageRef, 0, len(ids))
	for _, raw := range ids {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		host := defaultHost
		idStr := raw
		// Host-qualified form is "host/id"; bare numeric form uses defaultHost.
		if idx := strings.LastIndex(raw, "/"); idx > 0 {
			if _, err := strconv.ParseInt(raw[idx+1:], 10, 64); err == nil {
				host = raw[:idx]
				idStr = raw[idx+1:]
			}
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		out = append(out, store.PageRef{Host: host, ID: id})
	}
	return links.DedupPageRefs(out)
}

func hasLeadingTitleH1(markdown, title string) bool {
	md := strings.TrimSpace(strings.TrimPrefix(markdown, "\ufeff"))
	title = strings.TrimSpace(title)
	if md == "" || title == "" {
		return false
	}

	lines := strings.Split(md, "\n")
	if len(lines) == 0 {
		return false
	}

	first := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(first, "# ") {
		return false
	}

	firstTitle := strings.TrimSpace(strings.TrimPrefix(first, "# "))
	return strings.EqualFold(firstTitle, title)
}

// enqueueChildren adds extracted child pages to the queue
func (cs *CrawlSession) enqueueChildren(parentDepth int, children []store.PageRef) {
	childDepth := parentDepth + 1

	cs.mu.Lock()
	defer cs.mu.Unlock()

	for _, child := range children {
		key := child.Key()
		if !cs.visited[key] {
			cs.visited[key] = true
			cs.pendingWork.Add(1)
			select {
			case cs.queue <- queueItem{host: child.Host, pageID: child.ID, depth: childDepth}:
			default:
				// Queue is saturated: track and fail loud later instead of silently losing pages.
				cs.enqueueDrops++
				if len(cs.enqueueDropSample) < maxQueueDropSamples {
					cs.enqueueDropSample = append(cs.enqueueDropSample, queueDropSample{
						PageID: child.ID,
						Depth:  childDepth,
					})
				}
				cs.pendingWork.Done()
			}
		}
	}
}

// Stats returns crawl statistics
func (cs *CrawlSession) Stats() map[string]any {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	depthDist := make(map[int]int)
	linkCount := 0
	uniqueInternalTargets := make(map[string]struct{})
	externalSkipped := 0
	for _, page := range cs.results {
		depthDist[page.Depth]++
		linkCount += len(page.OutgoingLinks)
		for _, target := range page.OutgoingLinks {
			uniqueInternalTargets[target.Key()] = struct{}{}
		}
		externalSkipped += page.ExternalLinksSkipped
	}

	return map[string]any{
		"total_pages":             len(cs.results),
		"total_links":             linkCount,
		"unique_internal_targets": len(uniqueInternalTargets),
		"external_links_skipped":  externalSkipped,
		"queue_drops":             cs.enqueueDrops,
		"queue_drop_sample_count": len(cs.enqueueDropSample),
		"depth_distribution":      depthDist,
	}
}
