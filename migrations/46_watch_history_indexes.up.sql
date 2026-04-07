-- Partial index for the DISTINCT ON unwatched query in GetRecentlyWatched.
-- Covers: WHERE user_id = ? AND watched = false AND duration > 0
--         ORDER BY resource_id, updated_at DESC
CREATE INDEX IF NOT EXISTS watch_history_user_unwatched_idx
	ON public.watch_history (user_id, resource_id, updated_at DESC)
	WHERE watched = false AND duration > 0;

-- Partial index for the watched-resources aggregation query.
-- Covers: WHERE user_id = ? AND watched = true GROUP BY resource_id
CREATE INDEX IF NOT EXISTS watch_history_user_watched_resource_idx
	ON public.watch_history (user_id, resource_id)
	WHERE watched = true;

-- Episode lookup by resource_id (no index existed on the FK column).
CREATE INDEX IF NOT EXISTS idx_episode_resource_id
	ON public.episode (resource_id);
