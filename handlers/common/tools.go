package common

// Tool represents a tool page with its URL and i18n translation keys
// for the title, benefit, and description. These keys are resolved in
// templates via {{ t $.Lang .Title }}.
type Tool struct {
	Url         string
	Title       string
	Benefit     string
	Description string
	// Guide marks a page whose title is the user's question rather than a
	// capability ("How to Open a .torrent File" vs "Torrent → ZIP"). They
	// serve a different intent and are listed apart in the footer.
	Guide bool
}

// Tools contains the list of all available tool pages.
// Title, Benefit, and Description are i18n message keys.
var Tools = []Tool{
	{Url: "torrent-to-ddl", Title: "tool.torrentToDdl.title", Benefit: "tool.torrentToDdl.benefit", Description: "tool.torrentToDdl.description"},
	{Url: "torrent-to-zip", Title: "tool.torrentToZip.title", Benefit: "tool.torrentToZip.benefit", Description: "tool.torrentToZip.description"},
	{Url: "magnet-to-ddl", Title: "tool.magnetToDdl.title", Benefit: "tool.magnetToDdl.benefit", Description: "tool.magnetToDdl.description"},
	{Url: "magnet-to-torrent", Title: "tool.magnetToTorrent.title", Benefit: "tool.magnetToTorrent.benefit", Description: "tool.magnetToTorrent.description"},
	{Url: "torrent-to-stream", Title: "tool.torrentToStream.title", Benefit: "tool.torrentToStream.benefit", Description: "tool.torrentToStream.description"},
	// "torrent to mp4" is searched by people who think a .torrent is a video
	// file. The page corrects that premise rather than promising a
	// conversion we do not perform: the video inside plays as is, and the
	// original file downloads by direct link.
	{Url: "torrent-to-mp4", Title: "tool.torrentToMp4.title", Benefit: "tool.torrentToMp4.benefit", Description: "tool.torrentToMp4.description"},
	{Url: "watch-torrents-online", Title: "tool.watchTorrentsOnline.title", Benefit: "tool.watchTorrentsOnline.benefit", Description: "tool.watchTorrentsOnline.description"},
	{Url: "watch-torrents-ios", Title: "tool.watchTorrentsIos.title", Benefit: "tool.watchTorrentsIos.benefit", Description: "tool.watchTorrentsIos.description"},
	// Question-shaped landings. GSC, 28 days: the "how to open/download a
	// torrent file" queries bring ~7k impressions a month, land on the home
	// page at positions 6-8 and convert at 1-3%, while the one question that
	// has its own page (/watch-torrents-ios) converts at 7-10% from
	// position 4. The titles are phrased as the question for that reason —
	// what loses the click is a product headline answering a how-to query.
	{Url: "open-torrent-file", Title: "tool.openTorrentFile.title", Benefit: "tool.openTorrentFile.benefit", Description: "tool.openTorrentFile.description", Guide: true},
	{Url: "how-to-download-torrent-files", Title: "tool.howToDownloadTorrentFiles.title", Benefit: "tool.howToDownloadTorrentFiles.benefit", Description: "tool.howToDownloadTorrentFiles.description", Guide: true},
	{Url: "open-torrents-on-pc", Title: "tool.openTorrentsOnPc.title", Benefit: "tool.openTorrentsOnPc.benefit", Description: "tool.openTorrentsOnPc.description", Guide: true},
	{Url: "online-torrent-downloader", Title: "tool.onlineTorrentDownloader.title", Benefit: "tool.onlineTorrentDownloader.benefit", Description: "tool.onlineTorrentDownloader.description"},
	{Url: "stream-torrent-online", Title: "tool.streamTorrentOnline.title", Benefit: "tool.streamTorrentOnline.benefit", Description: "tool.streamTorrentOnline.description"},
	{Url: "torrent-player", Title: "tool.torrentPlayer.title", Benefit: "tool.torrentPlayer.benefit", Description: "tool.torrentPlayer.description"},
	{Url: "web-torrent-client", Title: "tool.webTorrentClient.title", Benefit: "tool.webTorrentClient.benefit", Description: "tool.webTorrentClient.description"},
	{Url: "cloud-torrent-client", Title: "tool.cloudTorrentClient.title", Benefit: "tool.cloudTorrentClient.benefit", Description: "tool.cloudTorrentClient.description"},
	{Url: "stremio-addons-online", Title: "tool.stremioAddonsOnline.title", Benefit: "tool.stremioAddonsOnline.benefit", Description: "tool.stremioAddonsOnline.description"},
	{Url: "webtor-stremio-addon", Title: "tool.webtorStremioAddon.title", Benefit: "tool.webtorStremioAddon.benefit", Description: "tool.webtorStremioAddon.description"},
}
