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

// DisableAccessToken disables the current access token
func (c *Client) DisableAccessToken(ctx context.Context) error {
	_, err := c.get(ctx, "/disable_access_token", nil)
	return err
}

// GetTime returns the server time
func (c *Client) GetTime(ctx context.Context) (*TimeResponse, error) {
	data, err := c.get(ctx, "/time", nil)
	if err != nil {
		return nil, err
	}
	var resp TimeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal time response: %w", err)
	}
	return &resp, nil
}

// GetTimeISO returns the server time in ISO format
func (c *Client) GetTimeISO(ctx context.Context) (*TimeISOResponse, error) {
	data, err := c.get(ctx, "/time/iso", nil)
	if err != nil {
		return nil, err
	}
	var resp TimeISOResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal time ISO response: %w", err)
	}
	return &resp, nil
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

// CheckLink checks if a link is supported and returns information about it
func (c *Client) CheckLink(ctx context.Context, link string, password string) (*CheckLinkResponse, error) {
	params := url.Values{}
	params.Set("link", link)
	if password != "" {
		params.Set("password", password)
	}
	data, err := c.post(ctx, "/unrestrict/check", params)
	if err != nil {
		return nil, err
	}
	var resp CheckLinkResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal check link response: %w", err)
	}
	return &resp, nil
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

// UnrestrictFolder unrestricts a folder link and returns all links within it
func (c *Client) UnrestrictFolder(ctx context.Context, link string) ([]string, error) {
	params := url.Values{}
	params.Set("link", link)
	data, err := c.post(ctx, "/unrestrict/folder", params)
	if err != nil {
		return nil, err
	}
	var links []string
	if err := json.Unmarshal(data, &links); err != nil {
		return nil, fmt.Errorf("failed to unmarshal folder links: %w", err)
	}
	return links, nil
}

// DecryptContainerFile decrypts a container file (DLC, CCF, CCF3, RSDF)
func (c *Client) DecryptContainerFile(ctx context.Context, fileContent []byte) ([]string, error) {
	// This would need multipart form data upload
	return nil, fmt.Errorf("container file upload not implemented")
}

// DecryptContainerLink decrypts a container file from a link
func (c *Client) DecryptContainerLink(ctx context.Context, link string) ([]string, error) {
	params := url.Values{}
	params.Set("link", link)
	data, err := c.post(ctx, "/unrestrict/containerLink", params)
	if err != nil {
		return nil, err
	}
	var links []string
	if err := json.Unmarshal(data, &links); err != nil {
		return nil, fmt.Errorf("failed to unmarshal container links: %w", err)
	}
	return links, nil
}

// GetTraffic returns traffic information for limited hosters
func (c *Client) GetTraffic(ctx context.Context) (*Traffic, error) {
	data, err := c.get(ctx, "/traffic", nil)
	if err != nil {
		return nil, err
	}
	var traffic Traffic
	if err := json.Unmarshal(data, &traffic); err != nil {
		return nil, fmt.Errorf("failed to unmarshal traffic: %w", err)
	}
	return &traffic, nil
}

// GetTrafficDetails returns detailed traffic information on used hosters
func (c *Client) GetTrafficDetails(ctx context.Context) ([]TrafficDetails, error) {
	data, err := c.get(ctx, "/traffic/details", nil)
	if err != nil {
		return nil, err
	}
	var details []TrafficDetails
	if err := json.Unmarshal(data, &details); err != nil {
		return nil, fmt.Errorf("failed to unmarshal traffic details: %w", err)
	}
	return details, nil
}

// GetStreamingTranscode returns transcoding links for a given file ID
func (c *Client) GetStreamingTranscode(ctx context.Context, id string) (*StreamingTranscode, error) {
	data, err := c.get(ctx, "/streaming/transcode/"+id, nil)
	if err != nil {
		return nil, err
	}
	var transcode StreamingTranscode
	if err := json.Unmarshal(data, &transcode); err != nil {
		return nil, fmt.Errorf("failed to unmarshal streaming transcode: %w", err)
	}
	return &transcode, nil
}

// GetStreamingMediaInfo returns media information for a given file ID
func (c *Client) GetStreamingMediaInfo(ctx context.Context, id string) (*MediaInfo, error) {
	data, err := c.get(ctx, "/streaming/mediaInfos/"+id, nil)
	if err != nil {
		return nil, err
	}
	var info MediaInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal media info: %w", err)
	}
	return &info, nil
}

// GetDownloads returns the user's downloads list
func (c *Client) GetDownloads(ctx context.Context, offset, limit int) ([]Download, error) {
	params := url.Values{}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	data, err := c.get(ctx, "/downloads", params)
	if err != nil {
		return nil, err
	}
	var downloads []Download
	if err := json.Unmarshal(data, &downloads); err != nil {
		return nil, fmt.Errorf("failed to unmarshal downloads: %w", err)
	}
	return downloads, nil
}

// DeleteDownload deletes a download from the downloads list
func (c *Client) DeleteDownload(ctx context.Context, id string) error {
	_, err := c.delete(ctx, "/downloads/delete/"+id, nil)
	return err
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

// GetTorrentsActiveCount returns the count of currently active torrents
func (c *Client) GetTorrentsActiveCount(ctx context.Context) (int, error) {
	data, err := c.get(ctx, "/torrents/activeCount", nil)
	if err != nil {
		return 0, err
	}
	var result struct {
		Nb    int `json:"nb"`
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return 0, fmt.Errorf("failed to unmarshal active count: %w", err)
	}
	return result.Nb, nil
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

// AddTorrent adds a torrent file
func (c *Client) AddTorrent(ctx context.Context, fileContent []byte, host string) (*TorrentAddResponse, error) {
	// This would need multipart form data upload
	return nil, fmt.Errorf("torrent file upload not implemented")
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
	// Convert file IDs to comma-separated string
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

// GetHosts returns the list of supported hosts
func (c *Client) GetHosts(ctx context.Context) ([]Host, error) {
	data, err := c.get(ctx, "/hosts", nil)
	if err != nil {
		return nil, err
	}
	var hosts []Host
	if err := json.Unmarshal(data, &hosts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hosts: %w", err)
	}
	return hosts, nil
}

// GetHostsStatus returns the status of all hosters
func (c *Client) GetHostsStatus(ctx context.Context) (map[string]HostStatus, error) {
	data, err := c.get(ctx, "/hosts/status", nil)
	if err != nil {
		return nil, err
	}
	var status map[string]HostStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hosts status: %w", err)
	}
	return status, nil
}

// GetHostsRegex returns all supported regex patterns
func (c *Client) GetHostsRegex(ctx context.Context) ([]string, error) {
	data, err := c.get(ctx, "/hosts/regex", nil)
	if err != nil {
		return nil, err
	}
	var regex []string
	if err := json.Unmarshal(data, &regex); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hosts regex: %w", err)
	}
	return regex, nil
}

// GetHostsRegexFolder returns all supported regex patterns for folder links
func (c *Client) GetHostsRegexFolder(ctx context.Context) ([]string, error) {
	data, err := c.get(ctx, "/hosts/regexFolder", nil)
	if err != nil {
		return nil, err
	}
	var regex []string
	if err := json.Unmarshal(data, &regex); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hosts regex folder: %w", err)
	}
	return regex, nil
}

// GetHostsDomains returns all supported domains
func (c *Client) GetHostsDomains(ctx context.Context) ([]string, error) {
	data, err := c.get(ctx, "/hosts/domains", nil)
	if err != nil {
		return nil, err
	}
	var domains []string
	if err := json.Unmarshal(data, &domains); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hosts domains: %w", err)
	}
	return domains, nil
}

// GetSettings returns current user settings
func (c *Client) GetSettings(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.get(ctx, "/settings", nil)
	if err != nil {
		return nil, err
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}
	return settings, nil
}

// UpdateSettings updates user settings
func (c *Client) UpdateSettings(ctx context.Context, settingName string, settingValue string) error {
	params := url.Values{}
	params.Set("setting_name", settingName)
	params.Set("setting_value", settingValue)
	_, err := c.post(ctx, "/settings/update", params)
	return err
}

// ConvertPoints converts fidelity points to premium days
func (c *Client) ConvertPoints(ctx context.Context) error {
	_, err := c.post(ctx, "/settings/convertPoints", nil)
	return err
}

// ChangePassword changes the user password
func (c *Client) ChangePassword(ctx context.Context) error {
	_, err := c.post(ctx, "/settings/changePassword", nil)
	return err
}

// GetAvatarFile returns the user avatar file
func (c *Client) GetAvatarFile(ctx context.Context) ([]byte, error) {
	return c.get(ctx, "/settings/avatarFile", nil)
}

// DeleteAvatarFile deletes the user avatar
func (c *Client) DeleteAvatarFile(ctx context.Context) error {
	_, err := c.delete(ctx, "/settings/avatarFile", nil)
	return err
}

// UploadAvatarFile uploads a user avatar
func (c *Client) UploadAvatarFile(ctx context.Context, fileContent []byte) error {
	// This would need multipart form data upload
	return fmt.Errorf("avatar file upload not implemented")
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
