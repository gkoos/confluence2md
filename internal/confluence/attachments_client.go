package confluence

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	model "github.com/ctreminiom/go-atlassian/v2/pkg/infra/models"
)

// GetPageAttachments fetches all attachments for a page via the v2 API.
func (c *Client) GetPageAttachments(ctx context.Context, pageID int64) ([]AttachmentData, error) {
	var all []AttachmentData
	cursor := ""

	options := &model.AttachmentParamsScheme{SerializeIDs: true}

	for {
		page, response, err := c.api.Attachment.Gets(ctx, int(pageID), "pages", options, cursor, 100)
		if err != nil {
			if response != nil {
				return nil, fmt.Errorf("request attachments (status %d): %w", response.Code, err)
			}
			return nil, fmt.Errorf("request attachments: %w", err)
		}

		for _, r := range page.Results {
			downloadURL := fmt.Sprintf("%s/wiki/api/v2/attachments/%s/download", c.baseURL, r.ID)
			all = append(all, AttachmentData{
				ID:            strings.TrimSpace(r.ID),
				PageID:        strings.TrimSpace(r.PageID),
				Filename:      strings.TrimSpace(r.Title),
				MediaType:     strings.TrimSpace(r.MediaType),
				FileSizeBytes: int64(r.FileSize),
				DownloadURL:   downloadURL,
				FileID:        strings.TrimSpace(r.FileID),
			})
		}

		if page.Links == nil || strings.TrimSpace(page.Links.Next) == "" {
			break
		}
		cursor = strings.TrimSpace(page.Links.Next)
	}

	return all, nil
}

// readWithLimit reads and validates the response body against a size limit.
// Returns an error if the response size exceeds the limit (detected by reading
// one extra byte and checking for EOF).
func readWithLimit(resp *http.Response, maxBytes int64) ([]byte, error) {
	contentLen := resp.ContentLength
	reader := io.Reader(resp.Body)

	// If maxBytes is set, enforce it
	if maxBytes > 0 {
		// Check Content-Length header if available
		if contentLen > maxBytes {
			return nil, fmt.Errorf("response size %d exceeds limit of %d bytes", contentLen, maxBytes)
		}

		// Read up to maxBytes + 1 to detect truncation
		limitReader := io.LimitReader(reader, maxBytes+1)
		data, err := io.ReadAll(limitReader)
		if err != nil {
			return nil, fmt.Errorf("read attachment: %w", err)
		}

		// If we got more than maxBytes, it was truncated
		if int64(len(data)) > maxBytes {
			return nil, fmt.Errorf("attachment response truncated: received %d bytes but limit is %d", len(data), maxBytes)
		}

		return data, nil
	}

	// No limit, read everything
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	return data, nil
}

// DownloadAttachment downloads binary attachment content.
// Discovery remains v2; binary retrieval follows the documented redirect endpoint.
// maxBytes limits the response size; 0 means no limit.
func (c *Client) DownloadAttachment(ctx context.Context, attachment AttachmentData, maxBytes int64) ([]byte, error) {
	if strings.TrimSpace(attachment.PageID) == "" {
		return nil, fmt.Errorf("download attachment %s: missing page ID", attachment.ID)
	}
	if strings.TrimSpace(attachment.ID) == "" {
		return nil, fmt.Errorf("download attachment: missing attachment ID")
	}

	redirectEndpoint := fmt.Sprintf("%s/wiki/rest/api/content/%s/child/attachment/%s/download", c.baseURL, attachment.PageID, attachment.ID)

	req, err := c.newAuthedRequest(ctx, http.MethodGet, redirectEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build attachment redirect request: %w", err)
	}

	transport := c.httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("request attachment redirect URI: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var downloadURL string
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		downloadURL = resolveNextEndpoint(c.baseURL, resp.Header.Get("Location"))
		if strings.TrimSpace(downloadURL) == "" {
			return nil, fmt.Errorf("attachment redirect missing Location header")
		}
	case http.StatusOK:
		// Direct 200 response (rare; usually redirects to CDN)
		data, err := readWithLimit(resp, maxBytes)
		return data, err
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("attachment redirect endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Only re-send credentials if the redirect stayed on the Confluence host.
	// Cross-host redirects (e.g. to a media CDN) use signed URLs and must not
	// receive our Basic Auth, mirroring net/http's own redirect behaviour.
	var fileReq *http.Request
	if sameHost(c.baseURL, downloadURL) {
		fileReq, err = c.newAuthedRequest(ctx, http.MethodGet, downloadURL, nil)
	} else {
		fileReq, err = http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("build attachment file request: %w", err)
	}

	fileResp, err := c.httpClient.Do(fileReq)
	if err != nil {
		return nil, fmt.Errorf("download attachment file: %w", err)
	}
	defer func() {
		_ = fileResp.Body.Close()
	}()

	if fileResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(fileResp.Body, 1024))
		return nil, fmt.Errorf("attachment file endpoint returned status %d: %s", fileResp.StatusCode, strings.TrimSpace(string(body)))
	}

	data, err := readWithLimit(fileResp, maxBytes)
	return data, err
}
