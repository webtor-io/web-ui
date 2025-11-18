package realdebrid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client represents a Real-Debrid API client
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiToken   string
}

// New creates a new Real-Debrid client with the provided HTTP client, base URL, and API token
func New(httpClient *http.Client, baseURL string, token string) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiToken:   token,
	}
}

// GetUser returns information about the current user
func (c *Client) GetUser(ctx context.Context) (*User, error) {
	data, err := c.get(ctx, "/user", nil)
	if err != nil {
		return nil, err
	}
	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}
	return &user, nil
}

// UnrestrictLink unrestricts a hoster link
func (c *Client) UnrestrictLink(ctx context.Context, link string, password string, remote bool) (*Download, error) {
	params := url.Values{}
	params.Set("link", link)
	if password != "" {
		params.Set("password", password)
	}
	if remote {
		params.Set("remote", "1")
	}
	data, err := c.post(ctx, "/unrestrict/link", params)
	if err != nil {
		return nil, err
	}
	var download Download
	if err := json.Unmarshal(data, &download); err != nil {
		return nil, fmt.Errorf("failed to unmarshal download: %w", err)
	}
	return &download, nil
}

// GetTorrents returns the user's torrents list
func (c *Client) GetTorrents(ctx context.Context, offset, limit int, activeFirst bool) ([]TorrentInfo, error) {
	params := url.Values{}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if activeFirst {
		params.Set("filter", "active")
	}
	data, err := c.get(ctx, "/torrents", params)
	if err != nil {
		return nil, err
	}
	var torrents []TorrentInfo
	if err := json.Unmarshal(data, &torrents); err != nil {
		return nil, fmt.Errorf("failed to unmarshal torrents: %w", err)
	}
	return torrents, nil
}

// GetAllTorrents returns all user's torrents with automatic pagination
// The RealDebrid API has a maximum limit of 5000 items per request
func (c *Client) GetAllTorrents(ctx context.Context, activeFirst bool) ([]TorrentInfo, error) {
	const maxLimit = 5000
	var allTorrents []TorrentInfo
	offset := 0

	for {
		torrents, err := c.GetTorrents(ctx, offset, maxLimit, activeFirst)
		if err != nil {
			return nil, err
		}

		if len(torrents) == 0 {
			break
		}

		allTorrents = append(allTorrents, torrents...)

		// If we got fewer results than the limit, we've reached the end
		if len(torrents) < maxLimit {
			break
		}

		offset += maxLimit
	}

	return allTorrents, nil
}

// GetTorrentInfo returns information about a specific torrent
func (c *Client) GetTorrentInfo(ctx context.Context, id string) (*TorrentInfo, error) {
	data, err := c.get(ctx, "/torrents/info/"+id, nil)
	if err != nil {
		return nil, err
	}
	var torrent TorrentInfo
	if err := json.Unmarshal(data, &torrent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal torrent info: %w", err)
	}
	return &torrent, nil
}

// GetTorrentsAvailableHosts returns available hosts for torrents
func (c *Client) GetTorrentsAvailableHosts(ctx context.Context) ([]Host, error) {
	data, err := c.get(ctx, "/torrents/availableHosts", nil)
	if err != nil {
		return nil, err
	}
	var hosts []Host
	if err := json.Unmarshal(data, &hosts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal available hosts: %w", err)
	}
	return hosts, nil
}

// AddMagnet adds a torrent via magnet link
func (c *Client) AddMagnet(ctx context.Context, magnet string, host string) (*TorrentAddResponse, error) {
	params := url.Values{}
	params.Set("magnet", magnet)
	if host != "" {
		params.Set("host", host)
	}
	data, err := c.post(ctx, "/torrents/addMagnet", params)
	if err != nil {
		return nil, err
	}
	var resp TorrentAddResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal add magnet response: %w", err)
	}
	return &resp, nil
}

// SelectTorrentFiles selects files from a torrent for download
func (c *Client) SelectTorrentFiles(ctx context.Context, id string, fileIDs []int) error {
	var fileIDsStr []string
	for _, fid := range fileIDs {
		fileIDsStr = append(fileIDsStr, strconv.Itoa(fid))
	}
	params := url.Values{}
	params.Set("files", strings.Join(fileIDsStr, ","))
	_, err := c.post(ctx, "/torrents/selectFiles/"+id, params)
	return err
}

// DeleteTorrent deletes a torrent from the torrents list
func (c *Client) DeleteTorrent(ctx context.Context, id string) error {
	_, err := c.delete(ctx, "/torrents/delete/"+id, nil)
	return err
}

// Helper methods for HTTP requests
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
			Error     string `json:"error"`
			ErrorCode int    `json:"error_code,omitempty"`
		}
		if err := json.Unmarshal(body, &apiError); err == nil && apiError.Error != "" {
			return nil, fmt.Errorf("API error (code %d): %s (error_code: %d)", resp.StatusCode, apiError.Error, apiError.ErrorCode)
		}
		return nil, fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	return body, nil
}
