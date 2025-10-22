package torbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

// Client is a TorBox API client
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiToken   string
}

// NewClient creates a new TorBox API client
func NewClient(httpClient *http.Client, baseURL, apiToken string) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		apiToken:   apiToken,
	}
}

// GetUser retrieves the current user's information
func (c *Client) GetUser(ctx context.Context) (*User, error) {
	body, err := c.get(ctx, "/v1/api/user/me", nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user")
	}

	var resp GetUserResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to parse user response")
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s", resp.Detail)
	}

	return &resp.Data, nil
}

// CreateTorrent creates a new torrent from a magnet link or torrent file
func (c *Client) CreateTorrent(ctx context.Context, magnet string) (*Torrent, error) {
	params := url.Values{}
	params.Set("magnet", magnet)

	body, err := c.post(ctx, "/v1/api/torrents/createtorrent", params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create torrent")
	}

	var resp CreateTorrentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to parse create torrent response")
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s", resp.Detail)
	}

	return &resp.Data, nil
}

// ListTorrents retrieves the list of user's torrents
func (c *Client) ListTorrents(ctx context.Context) ([]Torrent, error) {
	body, err := c.get(ctx, "/v1/api/torrents/mylist", nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list torrents")
	}

	var resp ListTorrentsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to parse list torrents response")
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s", resp.Detail)
	}

	return resp.Data, nil
}

// GetTorrentInfo retrieves information about a specific torrent by hash
func (c *Client) GetTorrentInfo(ctx context.Context, hash string) (*TorrentInfo, error) {
	params := url.Values{}
	params.Set("hash", hash)

	body, err := c.get(ctx, "/v1/api/torrents/torrentinfo", params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get torrent info")
	}

	var resp TorrentInfoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to parse torrent info response")
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s", resp.Detail)
	}

	return &resp.Data, nil
}

// ControlTorrent controls a torrent (pause, resume, delete, etc.)
func (c *Client) ControlTorrent(ctx context.Context, torrentID int, operation string) error {
	params := url.Values{}
	params.Set("torrent_id", fmt.Sprintf("%d", torrentID))
	params.Set("operation", operation)

	body, err := c.post(ctx, "/v1/api/torrents/controltorrent", params)
	if err != nil {
		return errors.Wrap(err, "failed to control torrent")
	}

	var resp struct {
		Success bool   `json:"success"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errors.Wrap(err, "failed to parse control torrent response")
	}

	if !resp.Success {
		return fmt.Errorf("API error: %s", resp.Detail)
	}

	return nil
}

// RequestDownloadLink requests a download link for a specific file
func (c *Client) RequestDownloadLink(ctx context.Context, torrentID, fileID int) (string, error) {
	params := url.Values{}
	params.Set("torrent_id", fmt.Sprintf("%d", torrentID))
	params.Set("file_id", fmt.Sprintf("%d", fileID))

	body, err := c.get(ctx, "/v1/api/torrents/requestdl", params)
	if err != nil {
		return "", errors.Wrap(err, "failed to request download link")
	}

	var resp struct {
		Success bool   `json:"success"`
		Detail  string `json:"detail"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", errors.Wrap(err, "failed to parse request download link response")
	}

	if !resp.Success {
		return "", fmt.Errorf("API error: %s", resp.Detail)
	}

	return resp.Data, nil
}

// CheckCached checks if torrents are cached by their hashes
func (c *Client) CheckCached(ctx context.Context, hashes []string, format string, listFiles bool) ([]CachedTorrent, error) {
	if len(hashes) == 0 {
		return nil, fmt.Errorf("at least one hash is required")
	}

	params := url.Values{}
	for _, hash := range hashes {
		params.Add("hash", hash)
	}
	if format != "" {
		params.Set("format", format)
	}
	params.Set("list_files", fmt.Sprintf("%t", listFiles))

	body, err := c.get(ctx, "/v1/api/torrents/checkcached", params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check cached torrents")
	}

	var resp CheckCachedResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to parse check cached response")
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s", resp.Detail)
	}

	return resp.Data, nil
}

// get performs a GET request
func (c *Client) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	urlStr := c.baseURL + path
	if params != nil && len(params) > 0 {
		urlStr += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return c.doRequest(req)
}

// post performs a POST request
func (c *Client) post(ctx context.Context, path string, params url.Values) ([]byte, error) {
	urlStr := c.baseURL + path

	var body io.Reader
	if params != nil {
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, "POST", urlStr, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if params != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	return c.doRequest(req)
}

// put performs a PUT request
func (c *Client) put(ctx context.Context, path string, params url.Values) ([]byte, error) {
	urlStr := c.baseURL + path

	var body io.Reader
	if params != nil {
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", urlStr, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if params != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	return c.doRequest(req)
}

// delete performs a DELETE request
func (c *Client) delete(ctx context.Context, path string, params url.Values) ([]byte, error) {
	urlStr := c.baseURL + path
	if params != nil && len(params) > 0 {
		urlStr += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return c.doRequest(req)
}

// doRequest executes an HTTP request
func (c *Client) doRequest(req *http.Request) ([]byte, error) {
	// Set authorization header if token is present
	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to parse error response
		var apiError struct {
			Success bool   `json:"success"`
			Detail  string `json:"detail"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(body, &apiError); err == nil {
			if apiError.Detail != "" {
				return nil, fmt.Errorf("API error (code %d): %s", resp.StatusCode, apiError.Detail)
			}
			if apiError.Error != "" {
				return nil, fmt.Errorf("API error (code %d): %s", resp.StatusCode, apiError.Error)
			}
		}
		return nil, fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	return body, nil
}
