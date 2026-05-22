package confluence

import "time"

// PageData holds full page content including storage format body.
type PageData struct {
	ID              string
	Title           string
	Version         int
	Seed            string
	StorageFormat   string
}

// FullPageData represents a page with all fields needed for crawling
type FullPageData struct {
	ID             int64
	Title          string
	Version        struct {
		Number int
	}
	Space struct {
		Key string
	}
	Body struct {
		Storage struct {
			Value string
		}
	}
	Links struct {
		Webui string
	}
}

// AttachmentData represents a Confluence page attachment.
type AttachmentData struct {
	ID          string
	PageID      string
	Filename    string
	MediaType   string
	FileSizeBytes int64
	DownloadURL string // absolute URL ready for authenticated download
}

// CommentData represents a Confluence page comment used by the export pipeline.
type CommentData struct {
	ID        string
	ParentID  string
	AuthorID  string
	Author    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Body      string
}

// PageStateData is a lightweight snapshot used for dirty/clean classification.
type PageStateData struct {
	ID                  int64
	Title               string
	Version             int
	AttachmentSignature string
}
