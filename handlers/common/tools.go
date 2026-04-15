package common

// Tool represents a tool page with its URL and i18n translation keys
// for the title, benefit, and description. These keys are resolved in
// templates via {{ t $.Lang .Title }}.
type Tool struct {
	Url         string
	Title       string
	Benefit     string
	Description string
}

// Tools contains the list of all available tool pages.
// Title, Benefit, and Description are i18n message keys.
var Tools = []Tool{
	{Url: "torrent-to-ddl", Title: "tool.torrentToDdl.title", Benefit: "tool.torrentToDdl.benefit", Description: "tool.torrentToDdl.description"},
	{Url: "torrent-to-zip", Title: "tool.torrentToZip.title", Benefit: "tool.torrentToZip.benefit", Description: "tool.torrentToZip.description"},
	{Url: "magnet-to-ddl", Title: "tool.magnetToDdl.title", Benefit: "tool.magnetToDdl.benefit", Description: "tool.magnetToDdl.description"},
	{Url: "magnet-to-torrent", Title: "tool.magnetToTorrent.title", Benefit: "tool.magnetToTorrent.benefit", Description: "tool.magnetToTorrent.description"},
	{Url: "torrent-to-stream", Title: "tool.torrentToStream.title", Benefit: "tool.torrentToStream.benefit", Description: "tool.torrentToStream.description"},
	{Url: "watch-torrents-online", Title: "tool.watchTorrentsOnline.title", Benefit: "tool.watchTorrentsOnline.benefit", Description: "tool.watchTorrentsOnline.description"},
	{Url: "watch-torrents-ios", Title: "tool.watchTorrentsIos.title", Benefit: "tool.watchTorrentsIos.benefit", Description: "tool.watchTorrentsIos.description"},
	{Url: "online-torrent-downloader", Title: "tool.onlineTorrentDownloader.title", Benefit: "tool.onlineTorrentDownloader.benefit", Description: "tool.onlineTorrentDownloader.description"},
	{Url: "stream-torrent-online", Title: "tool.streamTorrentOnline.title", Benefit: "tool.streamTorrentOnline.benefit", Description: "tool.streamTorrentOnline.description"},
	{Url: "torrent-player", Title: "tool.torrentPlayer.title", Benefit: "tool.torrentPlayer.benefit", Description: "tool.torrentPlayer.description"},
}
