package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	csrf "github.com/utrack/gin-csrf"
	"github.com/webtor-io/web-ui/services/api"
	vault "github.com/webtor-io/web-ui/services/vault"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

// TorrentStatus represents the current combined status of a torrent.
type TorrentStatus struct {
	State    string  `json:"state"`    // idle, caching, cached, vaulting, vaulted
	Progress float64 `json:"progress"` // 0-100 for caching/vaulting
	Seeders  int     `json:"seeders"`  // seed count for caching
}

// TorrentStatsData holds the relevant fields from a torrent stats event.
type TorrentStatsData struct {
	Total     int64
	Completed int
	Seeders   int
}

// resolveStatus is a pure function that determines the combined torrent status
// from vault DB state, vault API state, and torrent seeding stats.
// Priority: vaulted > vaulting > cached > caching > idle.
func resolveStatus(dbResource *vaultModels.Resource, apiResource *vault.Resource, stats *TorrentStatsData) *TorrentStatus {
	// Check vault state first (highest priority)
	vaultState := resolveVaultState(dbResource, apiResource)

	// Check caching state
	cachingState := resolveCachingState(stats)

	// Apply priority: vaulted > vaulting > cached > caching > idle
	if vaultState.State == "vaulted" {
		return vaultState
	}
	if vaultState.State == "vaulting" {
		if stats != nil {
			vaultState.Seeders = stats.Seeders
		}
		return vaultState
	}
	if cachingState.State == "cached" {
		return cachingState
	}
	if cachingState.State == "caching" {
		return cachingState
	}
	// Both are idle
	return &TorrentStatus{State: "idle"}
}

func resolveVaultState(dbResource *vaultModels.Resource, apiResource *vault.Resource) *TorrentStatus {
	if dbResource == nil {
		return &TorrentStatus{State: "idle"}
	}
	if dbResource.Vaulted {
		return &TorrentStatus{State: "vaulted"}
	}
	if !dbResource.Funded {
		return &TorrentStatus{State: "idle"}
	}
	// Funded but not vaulted — check API
	if apiResource == nil {
		return &TorrentStatus{State: "vaulting", Progress: 0}
	}
	switch apiResource.Status {
	case vault.StatusProcessing:
		return &TorrentStatus{State: "vaulting", Progress: apiResource.GetProgress()}
	case vault.StatusCompleted:
		return &TorrentStatus{State: "vaulted"}
	case vault.StatusQueued:
		return &TorrentStatus{State: "vaulting", Progress: 0}
	case vault.StatusFailed:
		return &TorrentStatus{State: "idle"}
	default:
		return &TorrentStatus{State: "idle"}
	}
}

func resolveCachingState(stats *TorrentStatsData) *TorrentStatus {
	if stats == nil {
		return &TorrentStatus{State: "idle"}
	}
	if stats.Total <= 0 {
		return &TorrentStatus{State: "idle"}
	}
	// If nothing has been downloaded yet, treat as idle — don't show "Caching 0%"
	// which would be misleading (the seeder may have been started just by our stats probe)
	if stats.Completed <= 0 {
		return &TorrentStatus{State: "idle", Seeders: stats.Seeders}
	}
	progress := float64(stats.Completed) / float64(stats.Total) * 100
	if progress >= 100 {
		return &TorrentStatus{State: "cached", Progress: 100, Seeders: stats.Seeders}
	}
	return &TorrentStatus{State: "caching", Progress: progress, Seeders: stats.Seeders}
}

// prepareInitialStatus computes the initial status for SSR (vault DB only, no SSE connection).
func (s *Handler) prepareInitialStatus(ctx context.Context, resourceID string) *TorrentStatus {
	if s.vault == nil {
		return &TorrentStatus{State: "idle"}
	}
	dbResource, err := s.vault.GetResource(ctx, resourceID)
	if err != nil {
		log.WithError(err).Warn("failed to get vault resource for initial status")
		return &TorrentStatus{State: "idle"}
	}
	return resolveStatus(dbResource, nil, nil)
}

// status is the SSE endpoint handler for real-time torrent status updates.
// Uses c.Stream() + c.SSEvent() like the job handler for proper proxy compatibility.
// All computation happens in a background goroutine; the callback only reads from a channel.
func (s *Handler) status(c *gin.Context) {
	// Validate CSRF token from query parameter (EventSource doesn't support custom headers)
	token := c.Query("_csrf")
	if token == "" || token != csrf.GetToken(c) {
		c.String(http.StatusForbidden, "CSRF token mismatch")
		return
	}

	resourceID := c.Param("resource_id")
	claims := api.GetClaimsFromContext(c)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache,no-store,no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// Channel for status updates from background goroutine
	statusCh := make(chan *TorrentStatus, 10)

	go s.statusLoop(ctx, claims, resourceID, statusCh)

	c.Stream(func(w io.Writer) bool {
		ticker := time.NewTicker(5 * time.Second)
		select {
		case <-ctx.Done():
			ticker.Stop()
			return false
		case <-ticker.C:
			c.SSEvent("ping", "")
			return true
		case status, ok := <-statusCh:
			if !ok {
				return false
			}
			c.SSEvent("message", status)
			return status.State != "vaulted"
		}
	})
}

// statusLoop runs in a background goroutine, computing status updates and sending them to the channel.
func (s *Handler) statusLoop(ctx context.Context, claims *api.Claims, resourceID string, out chan<- *TorrentStatus) {
	defer close(out)

	var statsCh <-chan api.EventData
	var lastStats *TorrentStatsData
	var lastJSON string
	var lastDBResource *vaultModels.Resource
	var lastAPIResource *vault.Resource

	type statsResult struct {
		ch  <-chan api.EventData
		msg string
	}
	statsChResult := make(chan statsResult, 1)

	// Start stats connection attempt (single attempt, no retry to avoid starting idle seeders)
	go func() {
		ch, msg := s.tryConnectStats(ctx, claims, resourceID)
		statsChResult <- statsResult{ch: ch, msg: msg}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	vaultTick := 0

	sendStatus := func() bool {
		status := resolveStatus(lastDBResource, lastAPIResource, lastStats)
		data, _ := json.Marshal(status)
		jsonStr := string(data)
		if jsonStr == lastJSON {
			return true
		}
		lastJSON = jsonStr
		select {
		case out <- status:
		case <-ctx.Done():
			return false
		}
		return status.State != "vaulted"
	}

	// Fetch vault state before first send to avoid idle→vaulted flicker
	if s.vault != nil {
		var err error
		lastDBResource, err = s.vault.GetResource(ctx, resourceID)
		if err != nil {
			log.WithError(err).Warn("failed to get vault resource for initial status")
		}
		if lastDBResource != nil && lastDBResource.Funded && !lastDBResource.Vaulted {
			lastAPIResource, err = s.vault.GetVaultAPIResource(ctx, resourceID)
			if err != nil {
				log.WithError(err).Warn("failed to get vault api resource for initial status")
			}
		}
	}

	// Wait for first stats connection attempt before sending initial status
	// to avoid idle→caching flicker
	initialSent := false

	for {
		select {
		case <-ctx.Done():
			return

		case res := <-statsChResult:
			statsCh = res.ch
			log.WithField("resourceID", resourceID).WithField("connected", res.ch != nil).WithField("msg", res.msg).Info("status: stats connection result")
			// If export says content is cached (no torrent_client_stat), mark as cached
			if res.msg == "cached" {
				lastStats = &TorrentStatsData{Total: 1, Completed: 1, Seeders: 0}
			}
			if !initialSent {
				if !sendStatus() {
					return
				}
				initialSent = true
			}

		case ev, ok := <-statsCh:
			if ok {
				lastStats = &TorrentStatsData{
					Total:     ev.Total,
					Completed: ev.Completed,
					Seeders:   ev.Peers,
				}
				log.WithField("resourceID", resourceID).WithField("completed", ev.Completed).WithField("total", ev.Total).WithField("peers", ev.Peers).Info("status: got stats event")
			} else {
				// Stats channel closed — seeder gone or connection dropped
				log.WithField("resourceID", resourceID).Warn("status: stats channel closed")
				lastStats = nil
				statsCh = nil
			}
			if !sendStatus() {
				return
			}

		case <-ticker.C:

			if s.vault != nil && vaultTick%2 == 0 {
				dbRes, err := s.vault.GetResource(ctx, resourceID)
				if err != nil {
					log.WithError(err).Warn("failed to get vault resource for status")
				} else {
					lastDBResource = dbRes
				}
				lastAPIResource = nil
				if lastDBResource != nil && lastDBResource.Funded && !lastDBResource.Vaulted {
					apiRes, err := s.vault.GetVaultAPIResource(ctx, resourceID)
					if err != nil {
						log.WithError(err).Warn("failed to get vault api resource for status")
					} else {
						lastAPIResource = apiRes
					}
				}
			}
			vaultTick++

			if !sendStatus() {
				return
			}
		}
	}
}

// tryConnectStats attempts to establish an SSE connection to the torrent-http-proxy
// for real-time torrent-level stats. Gets the root content ID from the list response,
// then uses ExportResourceContent to get the stat URL for the whole torrent.
func (s *Handler) tryConnectStats(ctx context.Context, claims *api.Claims, resourceID string) (<-chan api.EventData, string) {
	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Get root content ID from list response
	list, err := s.api.ListResourceContentCached(connCtx, claims, resourceID, &api.ListResourceContentArgs{
		Output: api.OutputList,
		Limit:  1,
	})
	if err != nil {
		msg := fmt.Sprintf("list failed: %v", err)
		log.WithError(err).WithField("resourceID", resourceID).Warn("status: " + msg)
		return nil, msg
	}

	// Use root item ID (ListResponse embeds ListItem with ID)
	rootID := list.ID
	if rootID == "" {
		return nil, "empty root ID"
	}

	// Get torrent-level export using root content ID
	exportResp, err := s.api.ExportResourceContent(connCtx, claims, resourceID, rootID, "")
	if err != nil {
		msg := fmt.Sprintf("export failed: %v", err)
		log.WithError(err).WithField("resourceID", resourceID).Warn("status: " + msg)
		return nil, msg
	}

	statItem, ok := exportResp.ExportItems["torrent_client_stat"]
	if !ok || statItem.URL == "" {
		// No stat URL means content is cached (rest-api skips torrent_client_stat for cached content)
		return nil, "cached"
	}

	// Check stats URL is accessible before opening SSE
	log.WithField("resourceID", resourceID).WithField("url", statItem.URL[:min(len(statItem.URL), 80)]).Info("status: connecting to stats SSE")

	// Open SSE connection to torrent-http-proxy (use parent ctx, not timeout ctx)
	ch, err := s.api.Stats(ctx, statItem.URL)
	if err != nil {
		// 404 from seeder means content is available (cached/vaulted)
		if err.Error() == "cached" {
			return nil, "cached"
		}
		msg := fmt.Sprintf("stats SSE failed: %v", err)
		log.WithError(err).WithField("resourceID", resourceID).Warn("status: " + msg)
		return nil, msg
	}
	log.WithField("resourceID", resourceID).Info("status: connected to torrent stats SSE")
	return ch, "connected"
}

