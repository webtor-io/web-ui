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
	// FAQ is how many question/answer pairs the page adds under the shared
	// FAQ, read as <AboutKey>.faq.qN.question / .answer, N = 1..FAQ (see
	// FaqPairs). They render after the shared questions, in the collapse
	// list and in the FAQPage JSON-LD alike; answers go through tHTML.
	FAQ int
}

// Tools contains the list of all available tool pages.
// Title, Benefit, and Description are i18n message keys.
var Tools = []Tool{
	{Url: "torrent-to-ddl", Title: "tool.torrentToDdl.title", Benefit: "tool.torrentToDdl.benefit", Description: "tool.torrentToDdl.description", FAQ: 5, Sections: []AboutSection{
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
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"},
			// The two conversions are one pair; each page points at the other.
			Link: &AboutLink{Url: "/torrent-to-magnet", TitleKey: "tool.torrentToMagnet.title"}},
		{Kind: AboutCompare, Key: "compare", Badge: "comparison", Accent: "cyan", Cols: []string{"magnet", "torrent"}, Footer: true},
		{Kind: AboutChecklist, Key: "useCases", Badge: "useCases", Accent: "pink", Alt: true, Items: 4, Extra: "reverse"},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
	// The reverse direction had no page of its own: the query landed on the
	// home page and on /magnet-to-torrent, whose "reverse" callout did not
	// hold it.
	{Url: "torrent-to-magnet", Title: "tool.torrentToMagnet.title", Benefit: "tool.torrentToMagnet.benefit", Description: "tool.torrentToMagnet.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"},
			Link: &AboutLink{Url: "/magnet-to-torrent", TitleKey: "tool.magnetToTorrent.title"}},
		{Kind: AboutCompare, Key: "compare", Badge: "comparison", Accent: "cyan", Cols: []string{"torrent", "magnet"}, Footer: true},
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
	// iOS sessions download a file more often than they stream one (47% vs
	// 19%), so the steps and the checklist cover both, and the free speed
	// cap is stated (from the catalog) rather than left to be found. The
	// comparison answers "utorrent for iphone": there is no uTorrent app in
	// the App Store, and uTorrent Lite — its browser version — ranks next to
	// this page on the core iOS queries (SERP snapshot 2026-09-21). Its
	// column says only what its own page and help center say (checked
	// 2026-09-23); recheck them before editing it.
	{Url: "watch-torrents-ios", Title: "tool.watchTorrentsIos.title", Benefit: "tool.watchTorrentsIos.benefit", Description: "tool.watchTorrentsIos.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2", "p3"}},
		{Kind: AboutCompare, Key: "utorrent", Badge: "comparison", Accent: "cyan", Cols: []string{"webtor", "lite"}, Cap: "cap", Footer: true},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Alt: true, Items: 4},
		{Kind: AboutChecklist, Key: "formats", Badge: "formats", Accent: "cyan", Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Alt: true, Paras: []string{"text"}},
	}},
	// "utorrent web" / "utorrent online" / "bittorrent web" — people who want
	// a torrent client that works in the browser and ask what else does that.
	// GSC, 28 days to 2026-10-13, all landing on the home page or the iOS
	// page: the cluster is 16.7k impressions, 1396 clicks, avg position 9.9
	// ("utorrent web" 1194 impr. at 11.8, "utorrent online" 1184 at 7.2,
	// "utorrent online downloader" 346 at 3.6 with 29.5% CTR, "bittorrent
	// web" 248 at 17.4); by country IN 2824, ES 444, US 438, RU 351, BR 238.
	// DataForSEO monthly volume for "utorrent web": IN 27100, BR 18100,
	// ES 8100, US 5400, FR 4400. The US SERP (2026-10-16) is weak — an AI
	// overview, superuser, reddit, a clone page, the vendor's forum. "utorrent
	// for iphone" stays with /watch-torrents-ios. The uTorrent Web column
	// says only what utorrent.com and its help center say (checked
	// 2026-10-16); recheck utorrent.com before editing it. The system-tray
	// sentence comes from help.utorrent.com/en/support/solutions/articles/
	// 29000022700 ("As long as µTorrent Web is running and available in
	// your system tray, your downloads will continue"). The devices
	// subtitle names uTorrent Lite because utorrent.com lists it for
	// Windows, macOS, ChromeOS, iPadOS and Android — so "no uTorrent Web
	// for Chromebook" alone would mislead. BitTorrent Web is described
	// without naming its maker: neither site says it is the same company.
	{Url: "utorrent-web-alternative", Title: "tool.utorrentWebAlternative.title", Benefit: "tool.utorrentWebAlternative.benefit", Description: "tool.utorrentWebAlternative.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2", "p3"},
			// The generic "no client" page answers the same need without the brand.
			Link: &AboutLink{Url: "/online-torrent-downloader", TitleKey: "tool.onlineTorrentDownloader.title"}},
		{Kind: AboutCompare, Key: "compare", Badge: "comparison", Accent: "cyan", Cols: []string{"webtor", "utorrentweb"}, Cap: "cap", Footer: true},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Alt: true, Items: 4},
		{Kind: AboutChecklist, Key: "devices", Badge: "devices", Accent: "pink", Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Alt: true, Paras: []string{"text"}},
	}},
	// Question-shaped landings. GSC, 28 days: the "how to open/download a
	// torrent file" queries bring ~7k impressions a month, land on the home
	// page at positions 6-8 and convert at 1-3%, while the one question that
	// has its own page (/watch-torrents-ios) converts at 7-10% from
	// position 4. The titles are phrased as the question for that reason —
	// what loses the click is a product headline answering a how-to query.
	//
	// Each guide carries its own FAQ (FAQ: 5), the question being the query
	// as people type it. GSC, 28 days to 2026-10-13: the shared FAQ answered
	// these questions in full on the home page, so Google served the HOME
	// page for them — "how to download torrented files" 1602 impr. / 36
	// clicks / pos 7.9, "how to open torrent files" 719, "how to open a
	// torrent file" 718, "how to open torrented files on pc" 375 at pos 8.0,
	// all at 1-2% CTR — while the guides got ~35 impressions each (only
	// /open-torrent-file ranked, 184 clicks). Since 2026-10-17 the four
	// shared how-to answers are one sentence plus the guide link, and the
	// full answer lives here only. The home page keeps "how to open
	// torrented files online" (130 impr., pos 1.4, 53% CTR) with its short
	// answer. Measure the per-query page split in GSC four weeks after
	// deploy.
	{Url: "open-torrent-file", Title: "tool.openTorrentFile.title", Benefit: "tool.openTorrentFile.benefit", Description: "tool.openTorrentFile.description", Guide: true, FAQ: 5, Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2", "p3"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutChecklist, Key: "formats", Badge: "formats", Accent: "cyan", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
	{Url: "how-to-download-torrent-files", Title: "tool.howToDownloadTorrentFiles.title", Benefit: "tool.howToDownloadTorrentFiles.benefit", Description: "tool.howToDownloadTorrentFiles.description", Guide: true, FAQ: 5, Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2", "p3"}},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Items: 4},
		{Kind: AboutChecklist, Key: "formats", Badge: "formats", Accent: "cyan", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
	{Url: "open-torrents-on-pc", Title: "tool.openTorrentsOnPc.title", Benefit: "tool.openTorrentsOnPc.benefit", Description: "tool.openTorrentsOnPc.description", Guide: true, FAQ: 5, Sections: []AboutSection{
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
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"},
			// p2 ends on devices that cannot run a torrent app; the brand page is the next step.
			Link: &AboutLink{Url: "/utorrent-web-alternative", TitleKey: "tool.utorrentWebAlternative.title"}},
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
	// Stremio users who use or want a debrid service — cached torrents served
	// over HTTP, so the device does not torrent — and look for another option.
	// DataForSEO, Google Ads monthly, 2026-10-17, US: "real debrid stremio"
	// 1900, "torbox stremio" 1000, "real debrid alternative" 880 +
	// "real-debrid alternative" 880, "best debrid service" 720, "what is
	// debrid" 720, "stremio debrid" 590, "free debrid" 480, "debrid
	// service(s)" 2400 each (navigational), "real debrid vs torbox" 390,
	// "torbox alternative" 90, "stremio without real debrid" 50, "stremio
	// without debrid" 40; GB: "real debrid stremio" 880, "real debrid
	// alternative" 390+390, "torbox stremio" 390. The live US SERP for "real
	// debrid alternative" (2026-10-16) is an AI overview, reddit, a forums
	// block, facebook posts, one listicle and YouTube — no product page. GSC
	// today: only the blog post real-debrid-and-torbox-integration ranks
	// (~500 impr., pos. ~10, "torbox vs real debrid"); the vs-comparison
	// stays with it. One generic slug: the demand splits across two brands
	// and a generic head of the same size, and a brand in the URL would
	// frame a service we ask users to connect as the thing to replace.
	//
	// Access model (owner, 2026-09-23; verified in services/link_resolver,
	// ResolveLink): the user's own enabled backends are tried first, in
	// profile order, with no tier check — a file they report cached plays
	// through them at no charge from Webtor, on any account; a backend error
	// or "not cached" falls through to Webtor's servers, which need a paid
	// plan (ErrPlanRequired, the paywall clip). ResolveLink's only caller is
	// the addon's /resolve handler, so backends matter in Stremio alone — the
	// web player never consults them. The page says "Playing through
	// Webtor's servers needs a paid Webtor plan." verbatim, quotes no trial
	// length, speed or price, and does not mention the site's free speed
	// cap (it does not apply to the addon) — hence no Cap line here. The
	// debrid column describes what any debrid is, never Real-Debrid or
	// TorBox in particular; the two are named only as services compared
	// with or connected to. Webtor is not a debrid: it resolves infohash +
	// file index only, supplies no catalog and "finds" nothing.
	{Url: "debrid-alternative", Title: "tool.debridAlternative.title", Benefit: "tool.debridAlternative.benefit", Description: "tool.debridAlternative.description", FAQ: 4, Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "stremio"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2", "p3"},
			// The install steps live on the addon page; this page is the "why".
			Link: &AboutLink{Url: "/webtor-stremio-addon", TitleKey: "tool.webtorStremioAddon.title"}},
		{Kind: AboutCompare, Key: "compare", Badge: "comparison", Accent: "cyan", Cols: []string{"webtor", "debrid"}, Footer: true},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Alt: true, Items: 4},
		// No devices section: it would be a verbatim copy of the addon page's,
		// which the explained Link already points to.
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
	// Release-subscriptions landing. Keyword research 2026-08-16 (DataForSEO):
	// the demand for this feature speaks "tracker / RSS / sonarr alternative",
	// not "notification" — "torrent rss feed" (260/mo, KD 0) and
	// "sonarr alternative" (90/mo, KD 3) both have forum-and-scripts SERPs,
	// while every notification-phrased query has zero volume and the big
	// "tracker" heads are drowned by the CBS series of that name. One page
	// targets the whole cluster; the comparison section is what carries the
	// "sonarr alternative" half.
	{Url: "torrent-rss-alternative", Title: "tool.torrentRssAlternative.title", Benefit: "tool.torrentRssAlternative.benefit", Description: "tool.torrentRssAlternative.description", Sections: []AboutSection{
		{Kind: AboutSteps, Key: "steps", Badge: "howItWorks", Accent: "pink", CTA: "discover"},
		{Kind: AboutProse, Key: "explained", Badge: "explained", Accent: "purple", Alt: true, Paras: []string{"p1", "p2"},
			// Subscriptions poll the sources this page teaches how to connect.
			Link: &AboutLink{Url: "/stremio-addons-online", TitleKey: "tool.stremioAddonsOnline.title"}},
		{Kind: AboutCompare, Key: "compare", Badge: "comparison", Accent: "cyan", Cols: []string{"webtor", "selfhosted"}, Footer: true},
		{Kind: AboutChecklist, Key: "benefits", Badge: "benefits", Accent: "pink", Alt: true, Items: 4},
		{Kind: AboutProse, Key: "safety", Badge: "safety", Accent: "purple", Paras: []string{"text"}},
	}},
}

// ToolByURL returns the tool page registered at url ("magnet-to-torrent", no
// leading slash), or nil. It is the whitelist for every request value that
// names a tool — the tool page's hidden `instruction` field, the `tool` query
// parameter on a resource page — so a forged value selects nothing and is
// never echoed back into a page.
func ToolByURL(url string) *Tool {
	if url == "" {
		return nil
	}
	for i := range Tools {
		if Tools[i].Url == url {
			return &Tools[i]
		}
	}
	return nil
}
