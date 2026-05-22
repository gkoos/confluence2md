package crawl

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gkoos/confluence2md/internal/confluence"
	"github.com/gkoos/confluence2md/internal/config"
	"github.com/gkoos/confluence2md/internal/convert"
	"github.com/gkoos/confluence2md/internal/links"
	"github.com/gkoos/confluence2md/internal/store"
)

// CrawledPage represents a page after conversion and link extraction
type CrawledPage struct {
	ID             int64
	Title          string
	Markdown       string
	Reused         bool
	Comments       []confluence.CommentData
	CommentCount   int
	CommentFetchError string
	StorageXML     string    // raw Confluence storage format XML
	CanonicalURL   string
	SpaceKey       string
	OutgoingLinks  []int64   // page IDs of all linked pages
	ExternalLinksSkipped int
	Version        int
	SourceURL      string
	CrawledAt      time.Time
	Depth          int
	FetchError     string // non-empty if fetch/convert failed

	Attachments          []confluence.AttachmentData
	AttachmentSignature  string
	AttachmentFetchError string
}

// NodeHandlerResult is the mode-specific output produced per traversed node.
// The traversal engine only depends on OutgoingLinks and FetchError.
type NodeHandlerResult struct {
	Page                 *CrawledPage
	OutgoingLinks        []int64
	FetchError           string
	Title                string
	ExternalLinksSkipped int
}

// CrawlNodeHandler processes a single traversed node.
type CrawlNodeHandler func(ctx context.Context, pageID int64, depth int) *NodeHandlerResult

// CrawlSession manages the full BFS traversal
type CrawlSession struct {
	client        *confluence.Client
	config        *config.Config
	maxDepth      int
	concurrency   int
	rateLimit     int // requests per minute
	seedSpaceKey  string // resolved alpha space key for title lookups
	nodeHandler   CrawlNodeHandler
	previousPages map[string]store.PageRecord

	// BFS state
	queue        chan queueItem
	visited      map[int64]bool
	results      map[int64]*CrawledPage
	mu            sync.RWMutex

	// Concurrency control
	semaphore    chan struct{}
	rateTicker   *time.Ticker
	rateLimitCh  chan struct{}
	
	// Work tracking
	pendingWork  sync.WaitGroup

	// Tracking
	totalFetched int
}

type queueItem struct {
	pageID int64
	depth  int
}

// NewCrawlSession creates a new BFS crawler session
func NewCrawlSession(client *confluence.Client, cfg *config.Config, seedSpaceKey string) *CrawlSession {
	cs := &CrawlSession{
		client:       client,
		config:       cfg,
		maxDepth:     cfg.Crawl.MaxDepth,
		concurrency:  cfg.Crawl.Concurrency,
		rateLimit:    cfg.Crawl.RateLimitRPM,
		seedSpaceKey: seedSpaceKey,

		queue:       make(chan queueItem, 10000), // large buffer to avoid blocking enqueue
		visited:     make(map[int64]bool),
		results:     make(map[int64]*CrawledPage),
		semaphore:   make(chan struct{}),
		rateLimitCh: make(chan struct{}, 1),
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

// EnableUpdatesMode switches traversal to updates-mode node handling.
func (cs *CrawlSession) EnableUpdatesMode(previousPages map[string]store.PageRecord) {
	cs.previousPages = previousPages
	cs.nodeHandler = cs.processUpdatesNode
}

// Run executes the full BFS crawl starting from seed pages
func (cs *CrawlSession) Run(ctx context.Context, seedPageIDs []int64) (map[int64]*CrawledPage, error) {
	// Initialize semaphore with concurrency limit
	cs.semaphore = make(chan struct{}, cs.concurrency)

	// Initialize rate limiter (tokens per minute)
	cs.rateTicker = time.NewTicker(time.Minute / time.Duration(cs.rateLimit))
	defer cs.rateTicker.Stop()

	// Start rate limiter goroutine
	go cs.runRateLimiter()

	// Start worker goroutines
	var workerWg sync.WaitGroup
	for i := 0; i < cs.concurrency; i++ {
		workerWg.Add(1)
		go cs.worker(ctx, &workerWg)
	}

	// Enqueue seed pages at depth 0
	for _, pageID := range seedPageIDs {
		cs.mu.Lock()
		if !cs.visited[pageID] {
			cs.visited[pageID] = true
			cs.pendingWork.Add(1)
			cs.queue <- queueItem{pageID: pageID, depth: 0}
		}
		cs.mu.Unlock()
	}

	// Wait for all pending work to complete
	cs.pendingWork.Wait()

	// Close queue to signal workers to exit
	close(cs.queue)
	workerWg.Wait()

	return cs.results, nil
}

// runRateLimiter ensures we don't exceed rate limit
func (cs *CrawlSession) runRateLimiter() {
	for range cs.rateTicker.C {
		select {
		case cs.rateLimitCh <- struct{}{}:
		default:
		}
	}
}

// worker processes items from the queue
func (cs *CrawlSession) worker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for item := range cs.queue {
		// Check depth limit
		if item.depth > cs.maxDepth {
			cs.pendingWork.Done()
			continue
		}

		// Acquire semaphore slot
		cs.semaphore <- struct{}{}

		// Respect rate limit
		<-cs.rateLimitCh

		// Process node via mode-specific callback.
		result := cs.nodeHandler(ctx, item.pageID, item.depth)
		if result == nil {
			result = &NodeHandlerResult{FetchError: "node handler returned nil result"}
		}

		title := result.Title
		if title == "" && result.Page != nil {
			title = result.Page.Title
		}

		cs.mu.Lock()
		if result.Page != nil {
			cs.results[item.pageID] = result.Page
		}
		cs.totalFetched++
		visited := len(cs.visited)
		cs.mu.Unlock()

		// Release semaphore BEFORE enqueuing children — otherwise all workers can
		// deadlock holding semaphore slots while blocking on a full queue channel.
		<-cs.semaphore

		childCount := 0
		if result.FetchError == "" && item.depth < cs.maxDepth {
			cs.enqueueChildren(item.depth, result.OutgoingLinks)
			childCount = len(result.OutgoingLinks)
		}

		depthPrefix := fmt.Sprintf("D%d", item.depth)
		if result.FetchError != "" {
			fmt.Printf("  [%s] ERR  %d — %s: %s\n", depthPrefix, item.pageID, title, result.FetchError)
		} else {
			fmt.Printf("  [%s] %3d/%-3d  %d — %s  (+%d links, ext-skip:%d, queue:%d)\n",
				depthPrefix, cs.totalFetched, visited, item.pageID, title, childCount, result.ExternalLinksSkipped, len(cs.queue))
		}

		cs.pendingWork.Done()
	}
}

// processFullNode fetches, converts, and extracts links for full mode.
func (cs *CrawlSession) processFullNode(ctx context.Context, pageID int64, depth int) *NodeHandlerResult {
	page := &CrawledPage{
		ID:        pageID,
		Depth:     depth,
		CrawledAt: time.Now(),
	}

	// Fetch page by ID
	fetchedPage, err := cs.client.GetPageByID(ctx, pageID)
	if err != nil {
		page.FetchError = fmt.Sprintf("fetch failed: %v", err)
		return &NodeHandlerResult{Page: page, FetchError: page.FetchError}
	}

	page.Title = fetchedPage.Title
	page.SourceURL = fetchedPage.Links.Webui
	page.CanonicalURL = fetchedPage.Links.Webui
	page.Version = fetchedPage.Version.Number
	page.SpaceKey = fetchedPage.Space.Key
	page.StorageXML = fetchedPage.Body.Storage.Value

	// Convert to Markdown
	markdown, err := convert.ToMarkdown(fetchedPage.Body.Storage.Value)
	if err != nil {
		page.FetchError = fmt.Sprintf("convert failed: %v", err)
		return &NodeHandlerResult{Page: page, FetchError: page.FetchError, Title: page.Title}
	}

	// Prepend page title as H1 only when it is not already present.
	if !hasLeadingTitleH1(markdown, page.Title) {
		markdown = fmt.Sprintf("# %s\n\n%s", page.Title, markdown)
	}

	page.Markdown = markdown

	// Fetch page comments (best-effort). Failure is non-fatal for page export.
	comments, err := cs.client.GetPageComments(ctx, pageID)
	if err != nil {
		page.CommentFetchError = fmt.Sprintf("comments fetch failed: %v", err)
	} else {
		page.Comments = comments
		page.CommentCount = len(comments)
	}

	// Fetch attachment metadata (best-effort). Failure is non-fatal for page export.
	if cs.config.Attachments.Download {
		attachments, err := cs.client.GetPageAttachments(ctx, pageID)
		if err != nil {
			page.AttachmentFetchError = fmt.Sprintf("attachments fetch failed: %v", err)
		} else {
			page.Attachments = attachments
			page.AttachmentSignature = attachmentSignatureFromData(attachments)
		}
	}

	// Extract links from storage XML — two sources:
	// 1. <a href> elements: yield numeric IDs directly from URL path
	// 2. <ri:page ri:content-title> elements: yield page titles that need API resolution
	page.OutgoingLinks, page.ExternalLinksSkipped = links.ExtractPageIDsFromStorageXMLWithStats(fetchedPage.Body.Storage.Value, cs.config.BaseURL())

	// Resolve title-based links to page IDs via CQL search
	// Use the seed space key (alphanumeric) — page.SpaceKey is a numeric ID from the API
	spaceKeyForLookup := cs.seedSpaceKey
	if spaceKeyForLookup == "" {
		spaceKeyForLookup = page.SpaceKey
	}
	titles := links.ExtractLinkedTitlesFromStorageXML(fetchedPage.Body.Storage.Value)
	if len(titles) > 0 {
		resolved := cs.resolveTitlesToIDs(ctx, titles, spaceKeyForLookup)
		page.OutgoingLinks = links.DedupPageIDs(append(page.OutgoingLinks, resolved...))
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
func (cs *CrawlSession) processUpdatesNode(ctx context.Context, pageID int64, depth int) *NodeHandlerResult {
	pageIDStr := strconv.FormatInt(pageID, 10)
	previous, exists := cs.previousPages[pageIDStr]

	state, err := cs.client.GetPageState(ctx, pageID, cs.config.Attachments.Download)
	if err != nil {
		// Conservative fallback: unknown state is treated as dirty.
		return cs.processFullNode(ctx, pageID, depth)
	}
	if state == nil || strings.TrimSpace(state.Title) == "" {
		// Conservative fallback for incomplete lightweight state.
		return cs.processFullNode(ctx, pageID, depth)
	}

	if !exists {
		return cs.processFullNode(ctx, pageID, depth)
	}

	if isDirtyComparedToPrevious(previous, state, cs.config.Attachments.Download) {
		return cs.processFullNode(ctx, pageID, depth)
	}

	outgoing := parseOutgoingLinkIDs(previous.OutgoingLinks)
	cleanPage := &CrawledPage{
		ID:                  pageID,
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

func parseOutgoingLinkIDs(ids []string) []int64 {
	out := make([]int64, 0, len(ids))
	for _, raw := range ids {
		parsed, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || parsed <= 0 {
			continue
		}
		out = append(out, parsed)
	}
	return links.DedupPageIDs(out)
}

func attachmentSignatureFromData(attachments []confluence.AttachmentData) string {
	if len(attachments) == 0 {
		return "none"
	}

	parts := make([]string, 0, len(attachments))
	for _, a := range attachments {
		parts = append(parts, strings.Join([]string{
			strings.TrimSpace(a.ID),
			strings.TrimSpace(a.Filename),
			strings.TrimSpace(a.MediaType),
			strconv.FormatInt(a.FileSizeBytes, 10),
		}, "|"))
	}

	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// resolveTitlesToIDs resolves a list of page titles to numeric page IDs via CQL.
// Errors for individual titles are logged but don't abort the crawl.
func (cs *CrawlSession) resolveTitlesToIDs(ctx context.Context, titles []string, spaceKey string) []int64 {
	var ids []int64
	for _, title := range titles {
		pageURL, err := cs.client.ResolvePageURLByTitle(ctx, title, spaceKey)
		if err != nil {
			// Non-fatal — leave the title-based link as-is in the markdown
			continue
		}
		id := links.ExtractPageIDFromURL(pageURL)
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
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
func (cs *CrawlSession) enqueueChildren(parentDepth int, childPageIDs []int64) {
	childDepth := parentDepth + 1

	cs.mu.Lock()
	defer cs.mu.Unlock()

	for _, childID := range childPageIDs {
		if !cs.visited[childID] {
			cs.visited[childID] = true
			cs.pendingWork.Add(1)
			select {
			case cs.queue <- queueItem{pageID: childID, depth: childDepth}:
			default:
				// queue full, skip (should rarely happen with buffered queue)
				cs.pendingWork.Done()
			}
		}
	}
}

// GetResults returns the accumulated crawl results
func (cs *CrawlSession) GetResults() map[int64]*CrawledPage {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.results
}

// Stats returns crawl statistics
func (cs *CrawlSession) Stats() map[string]interface{} {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	depthDist := make(map[int]int)
	linkCount := 0
	uniqueInternalTargets := make(map[int64]struct{})
	externalSkipped := 0
	for _, page := range cs.results {
		depthDist[page.Depth]++
		linkCount += len(page.OutgoingLinks)
		for _, targetID := range page.OutgoingLinks {
			uniqueInternalTargets[targetID] = struct{}{}
		}
		externalSkipped += page.ExternalLinksSkipped
	}

	return map[string]interface{}{
		"total_pages":              len(cs.results),
		"total_links":              linkCount,
		"unique_internal_targets":  len(uniqueInternalTargets),
		"external_links_skipped":   externalSkipped,
		"depth_distribution":       depthDist,
	}
}
