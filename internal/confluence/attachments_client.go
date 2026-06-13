package confluence

import (
	"context"
	"fmt"
	"io"
	"strings"
	"net/http"

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

// DownloadAttachment downloads binary attachment content.
// Discovery remains v2; binary retrieval follows the documented redirect endpoint.
func (c *Client) DownloadAttachment(ctx context.Context, attachment AttachmentData) ([]byte, error) {
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
		const maxBytes = 100 * 1024 * 1024
		return io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("attachment redirect endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	fileReq, err := c.newAuthedRequest(ctx, http.MethodGet, downloadURL, nil)
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

	const maxBytes = 100 * 1024 * 1024
	return io.ReadAll(io.LimitReader(fileResp.Body, maxBytes))
}
