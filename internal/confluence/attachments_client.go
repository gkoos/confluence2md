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

// copyWithLimit streams src into dst, enforcing an optional size limit.
// When maxBytes > 0 and Content-Length already exceeds the limit the function
// returns an error before reading any body bytes. If the response body itself
// turns out to be larger than maxBytes an error is returned after the copy.
func copyWithLimit(dst io.Writer, resp *http.Response, maxBytes int64) error {
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return fmt.Errorf("response size %d exceeds limit of %d bytes", resp.ContentLength, maxBytes)
	}

	var n int64
	var err error
	if maxBytes > 0 {
		// Copy one extra byte so we can detect an oversize body even when
		// Content-Length was absent or lied about.
		n, err = io.CopyN(dst, resp.Body, maxBytes+1)
		if err != nil && err != io.EOF {
			return fmt.Errorf("read attachment: %w", err)
		}
		if n > maxBytes {
			return fmt.Errorf("attachment response truncated: received more than %d bytes (limit)", maxBytes)
		}
	} else {
		_, err = io.Copy(dst, resp.Body)
		if err != nil {
			return fmt.Errorf("read attachment: %w", err)
		}
	}
	return nil
}

// DownloadAttachment streams binary attachment content into dst.
// Discovery remains v2; binary retrieval follows the documented redirect endpoint.
// maxBytes limits the response size; 0 means no limit.
func (c *Client) DownloadAttachment(ctx context.Context, attachment AttachmentData, maxBytes int64, dst io.Writer) error {
	if strings.TrimSpace(attachment.PageID) == "" {
		return fmt.Errorf("download attachment %s: missing page ID", attachment.ID)
	}
	if strings.TrimSpace(attachment.ID) == "" {
		return fmt.Errorf("download attachment: missing attachment ID")
	}

	redirectEndpoint := fmt.Sprintf("%s/wiki/rest/api/content/%s/child/attachment/%s/download", c.baseURL, attachment.PageID, attachment.ID)

	req, err := c.newAuthedRequest(ctx, http.MethodGet, redirectEndpoint, nil)
	if err != nil {
		return fmt.Errorf("build attachment redirect request: %w", err)
	}

	transport := c.httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("request attachment redirect URI: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var downloadURL string
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		downloadURL = resolveNextEndpoint(c.baseURL, resp.Header.Get("Location"))
		if strings.TrimSpace(downloadURL) == "" {
			return fmt.Errorf("attachment redirect missing Location header")
		}
	case http.StatusOK:
		// Direct 200 response (rare; usually redirects to CDN)
		return copyWithLimit(dst, resp, maxBytes)
	default:
		return fmt.Errorf("attachment redirect endpoint: %w", readAPIError(resp))
	}

	// Only re-send credentials if the redirect stayed on the Confluence host.
	// Cross-host redirects (e.g. to a media CDN) use signed URLs and must not
	// receive our Basic Auth, mirroring net/http's own redirect behaviour.
	fileReq, err := c.newConditionallyAuthedRequest(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("build attachment file request: %w", err)
	}

	fileResp, err := c.httpClient.Do(fileReq)
	if err != nil {
		return fmt.Errorf("download attachment file: %w", err)
	}
	defer func() {
		_ = fileResp.Body.Close()
	}()

	if fileResp.StatusCode != http.StatusOK {
		return fmt.Errorf("attachment file endpoint: %w", readAPIError(fileResp))
	}

	return copyWithLimit(dst, fileResp, maxBytes)
}
