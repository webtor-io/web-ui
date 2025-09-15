package stremio

import stremioTypes "github.com/webtor-io/web-ui/services/stremio"

// Re-export types from services/stremio to maintain backward compatibility
type StreamItem = stremioTypes.StreamItem
type StreamsResponse = stremioTypes.StreamsResponse
type MetaItem = stremioTypes.MetaItem
type VideoItem = stremioTypes.VideoItem
type MetasResponse = stremioTypes.MetasResponse
type MetaResponse = stremioTypes.MetaResponse
type CatalogItem = stremioTypes.CatalogItem
type Manifest = stremioTypes.Manifest
type BehaviorHints = stremioTypes.BehaviorHints
type ConfigOption = stremioTypes.ConfigOption
