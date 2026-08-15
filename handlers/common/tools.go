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
	// Sections is the page body, rendered by
	// templates/partials/about/sections.html. See about.go.
	Sections []AboutSection
}

// Tools contains the list of all available tool pages.
// Title, Benefit, and Description are i18n message keys.
var Tools = []Tool{
	{Url: "torrent-to-ddl", Title: "tool.torrentToDdl.title", Benefit: "tool.torrentToDdl.benefit", Description: "tool.torrentToDdl.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutCompare, Key: "compare", Badge: "comparison", Accent: "cyan", Cols: []string{"torrent", "ddl"}, Footer: true},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Alt: true, Paras: []string{"text"}},
		{Kind: AboutChecklist, Key: "video", Badge: "videoStreaming", Accent: "pink", Items: 4, Footer: true},
	}},
	{Url: "torrent-to-zip", Title: "tool.torrentToZip.title", Benefit: "tool.torrentToZip.benefit", Description: "tool.torrentToZip.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutCompare, Key: "compare", Badge: "comparison", Accent: "cyan", Alt: true, Cols: []string{"zip", "individual"}},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"p1", "p2"}},
		{Kind: AboutCompare, Key: "formats", Badge: "comparison", Icon: "download", Accent: "pink", Alt: true, Cols: []string{"tar", "zip"}, Note: true},
	}},
	{Url: "magnet-to-ddl", Title: "tool.magnetToDdl.title", Benefit: "tool.magnetToDdl.benefit", Description: "tool.magnetToDdl.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutCompare, Key: "compare", Badge: "comparison", Accent: "cyan", Cols: []string{"magnet", "torrent"}, Footer: true},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Alt: true, Paras: []string{"text"}},
		{Kind: AboutChecklist, Key: "video", Badge: "videoStreaming", Accent: "pink", Items: 4, Footer: true},
	}},
	{Url: "magnet-to-torrent", Title: "tool.magnetToTorrent.title", Benefit: "tool.magnetToTorrent.benefit", Description: "tool.magnetToTorrent.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutCompare, Key: "compare", Badge: "comparison", Accent: "cyan", Cols: []string{"magnet", "torrent"}, Footer: true},
		{Kind: AboutChecklist, Key: "useCases", Badge: "useCases", Accent: "pink", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
	{Url: "torrent-to-stream", Title: "tool.torrentToStream.title", Benefit: "tool.torrentToStream.benefit", Description: "tool.torrentToStream.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Alt: true, Paras: []string{"text"}},
		{Kind: AboutChecklist, Key: "devices", Badge: "devices", Accent: "pink", Items: 4},
	}},
	// "torrent to mp4" is searched by people who think a .torrent is a video
	// file. The page corrects that premise rather than promising a
	// conversion we do not perform: the video inside plays as is, and the
	// original file downloads by direct link.
	{Url: "torrent-to-mp4", Title: "tool.torrentToMp4.title", Benefit: "tool.torrentToMp4.benefit", Description: "tool.torrentToMp4.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Alt: true, Paras: []string{"text"}},
		{Kind: AboutChecklist, Key: "devices", Badge: "devices", Accent: "pink", Items: 4},
	}},
	{Url: "watch-torrents-online", Title: "tool.watchTorrentsOnline.title", Benefit: "tool.watchTorrentsOnline.benefit", Description: "tool.watchTorrentsOnline.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Alt: true, Paras: []string{"text"}},
		{Kind: AboutChecklist, Key: "devices", Badge: "devices", Accent: "pink", Items: 4},
	}},
	{Url: "watch-torrents-ios", Title: "tool.watchTorrentsIos.title", Benefit: "tool.watchTorrentsIos.benefit", Description: "tool.watchTorrentsIos.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2", "p3"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutChecklist, Key: "formats", Badge: "formats", Accent: "cyan", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
	// Question-shaped landings. GSC, 28 days: the "how to open/download a
	// torrent file" queries bring ~7k impressions a month, land on the home
	// page at positions 6-8 and convert at 1-3%, while the one question that
	// has its own page (/watch-torrents-ios) converts at 7-10% from
	// position 4. The titles are phrased as the question for that reason —
	// what loses the click is a product headline answering a how-to query.
	{Url: "open-torrent-file", Title: "tool.openTorrentFile.title", Benefit: "tool.openTorrentFile.benefit", Description: "tool.openTorrentFile.description", Guide: true, Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2", "p3"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutChecklist, Key: "formats", Badge: "formats", Accent: "cyan", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
	{Url: "how-to-download-torrent-files", Title: "tool.howToDownloadTorrentFiles.title", Benefit: "tool.howToDownloadTorrentFiles.benefit", Description: "tool.howToDownloadTorrentFiles.description", Guide: true, Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2", "p3"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutChecklist, Key: "formats", Badge: "formats", Accent: "cyan", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
	{Url: "open-torrents-on-pc", Title: "tool.openTorrentsOnPc.title", Benefit: "tool.openTorrentsOnPc.benefit", Description: "tool.openTorrentsOnPc.description", Guide: true, Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2", "p3"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutChecklist, Key: "formats", Badge: "formats", Accent: "cyan", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
	{Url: "online-torrent-downloader", Title: "tool.onlineTorrentDownloader.title", Benefit: "tool.onlineTorrentDownloader.benefit", Description: "tool.onlineTorrentDownloader.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutCompare, Key: "compare", Badge: "comparison", Accent: "cyan", Cols: []string{"online", "client"}, Footer: true},
		{Kind: AboutChecklist, Key: "useCases", Badge: "useCases", Accent: "pink", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"p1", "p2"}},
	}},
	{Url: "stream-torrent-online", Title: "tool.streamTorrentOnline.title", Benefit: "tool.streamTorrentOnline.benefit", Description: "tool.streamTorrentOnline.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Alt: true, Paras: []string{"text"}},
		{Kind: AboutChecklist, Key: "devices", Badge: "devices", Accent: "pink", Items: 4},
	}},
	{Url: "torrent-player", Title: "tool.torrentPlayer.title", Benefit: "tool.torrentPlayer.benefit", Description: "tool.torrentPlayer.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Alt: true, Paras: []string{"text"}},
		{Kind: AboutChecklist, Key: "devices", Badge: "devices", Accent: "pink", Items: 4},
	}},
	{Url: "web-torrent-client", Title: "tool.webTorrentClient.title", Benefit: "tool.webTorrentClient.benefit", Description: "tool.webTorrentClient.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
	}},
	{Url: "cloud-torrent-client", Title: "tool.cloudTorrentClient.title", Benefit: "tool.cloudTorrentClient.benefit", Description: "tool.cloudTorrentClient.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutChecklist, Key: "devices", Badge: "devices", Accent: "pink", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
	{Url: "stremio-addons-online", Title: "tool.stremioAddonsOnline.title", Benefit: "tool.stremioAddonsOnline.benefit", Description: "tool.stremioAddonsOnline.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"},
			// The addon page answers the same query one step further along.
			Link: &AboutLink{Url: "/webtor-stremio-addon", TitleKey: "tool.webtorStremioAddon.title"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Alt: true, Paras: []string{"text"}},
		{Kind: AboutChecklist, Key: "devices", Badge: "devices", Accent: "pink", Items: 4},
	}},
	{Url: "webtor-stremio-addon", Title: "tool.webtorStremioAddon.title", Benefit: "tool.webtorStremioAddon.benefit", Description: "tool.webtorStremioAddon.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "stremio"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4, Extra: "indexers"},
		{Kind: AboutChecklist, Key: "devices", Badge: "devices", Accent: "pink", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
}
