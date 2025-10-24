package torbox

import (
	"bytes"
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
func (s *Client) GetUser(ctx context.Context) (*User, error) {
	body, err := s.get(ctx, "/v1/api/user/me", nil)
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
func (s *Client) CreateTorrent(ctx context.Context, magnet string) (*CreateTorrentData, error) {
	params := url.Values{}
	params.Set("magnet", magnet)
	params.Set("add_only_if_cached", "true")

	body, err := s.post(ctx, "/v1/api/torrents/createtorrent", params)
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
func (s *Client) ListTorrents(ctx context.Context, id int) ([]Torrent, error) {
	params := url.Values{}
	params.Set("bypass_cache", "true")
	if id > 0 {
		params.Set("id", fmt.Sprintf("%d", id))
	}
	body, err := s.get(ctx, "/v1/api/torrents/mylist", params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list torrents")
	}

	if id == 0 {
		var resp ListTorrentsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, errors.Wrap(err, "failed to parse list torrents response")
		}

		if !resp.Success {
			return nil, fmt.Errorf("API error: %s", resp.Detail)
		}

		return resp.Data, nil
	} else {
		var resp ListTorrentResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, errors.Wrap(err, "failed to parse list torrents response")
		}

		if !resp.Success {
			return nil, fmt.Errorf("API error: %s", resp.Detail)
		}

		return []Torrent{resp.Data}, nil
	}
}

// ControlTorrent controls a torrent (pause, resume, delete, etc.)
func (s *Client) ControlTorrent(ctx context.Context, torrentID int, operation string) error {
	// per API docs, this endpoint expects a JSON body
	payload := map[string]any{
		"operation":  operation,
		"torrent_id": torrentID,
		"all":        false,
	}

	body, err := s.postJSON(ctx, "/v1/api/torrents/controltorrent", payload)
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
func (s *Client) RequestDownloadLink(ctx context.Context, torrentID, fileID int) (string, error) {
	params := url.Values{}
	params.Set("torrent_id", fmt.Sprintf("%d", torrentID))
	params.Set("file_id", fmt.Sprintf("%d", fileID))
	params.Set("token", s.apiToken)

	body, err := s.get(ctx, "/v1/api/torrents/requestdl", params)
	if err != nil {
		return "", errors.Wrap(err, "failed to request download link for cached torrent")
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
func (s *Client) CheckCached(ctx context.Context, hashes []string, format string, listFiles bool) ([]CachedTorrent, error) {
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

	body, err := s.get(ctx, "/v1/api/torrents/checkcached", params)
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

	// Convert map to array and populate hash field from map keys
	result := make([]CachedTorrent, 0, len(resp.Data))
	for hash, torrent := range resp.Data {
		torrent.Hash = hash
		result = append(result, torrent)
	}

	return result, nil
}

// get performs a GET request
func (s *Client) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	urlStr := s.baseURL + path
	if params != nil && len(params) > 0 {
		urlStr += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return s.doRequest(req)
}

// post performs a POST request
func (s *Client) post(ctx context.Context, path string, params url.Values) ([]byte, error) {
	urlStr := s.baseURL + path

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

	return s.doRequest(req)
}

// postJSON performs a POST request with a JSON body
func (s *Client) postJSON(ctx context.Context, path string, payload any) ([]byte, error) {
	urlStr := s.baseURL + path

	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", urlStr, bytes.NewBuffer(b))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return s.doRequest(req)
}

// put performs a PUT request
func (s *Client) put(ctx context.Context, path string, params url.Values) ([]byte, error) {
	urlStr := s.baseURL + path

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

	return s.doRequest(req)
}

// delete performs a DELETE request
func (s *Client) delete(ctx context.Context, path string, params url.Values) ([]byte, error) {
	urlStr := s.baseURL + path
	if params != nil && len(params) > 0 {
		urlStr += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return s.doRequest(req)
}

// doRequest executes an HTTP request
func (s *Client) doRequest(req *http.Request) ([]byte, error) {
	// Set authorization header if token is present
	if s.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiToken)
	}

	resp, err := s.httpClient.Do(req)
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
