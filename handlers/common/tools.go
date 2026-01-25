package common

// Tool represents a tool page with its URL and display title
type Tool struct {
	Url         string
	Title       string
	Benefit     string
	Description string
}

// Tools contains the list of all available tool pages
var Tools = []Tool{
	{
		Url:         "torrent-to-ddl",
		Title:       "Torrent → DDL",
		Benefit:     "Convert Torrent to Direct Download Link",
		Description: "Convert torrents to direct download links in your browser. Download files from torrents with cloud-based processing and no local client required.",
	},
	{
		Url:         "torrent-to-zip",
		Title:       "Torrent → ZIP",
		Benefit:     "Download Torrent Files as ZIP Archive",
		Description: "Download torrent files as a ZIP archive directly in your browser. Cloud-based torrent to ZIP conversion for fast and reliable access.",
	},
	{
		Url:         "magnet-to-ddl",
		Title:       "Magnet → DDL",
		Benefit:     "Convert Magnet Link to Direct Download Link",
		Description: "Convert magnet links to direct download links online. Process magnet links in your browser using secure cloud-based torrent handling.",
	},
	{
		Url:         "magnet-to-torrent",
		Title:       "Magnet → Torrent",
		Benefit:     "Convert Magnet Link to .Torrent File",
		Description: "Convert magnet links to .torrent files in your browser. Generate torrent files online with cloud-based processing.",
	},
	{
		Url:         "watch-torrents-online",
		Title:       "Watch Torrents Online",
		Benefit:     "Stream Torrent Videos in Browser",
		Description: "Stream torrent videos directly in your browser without installing software. Web-based torrent streaming with cloud processing.",
	},
	{
		Url:         "watch-torrents-ios",
		Title:       "Watch Torrents on iOS",
		Benefit:     "Stream Torrent to on iPhone / iPad – No Apps Needed",
		Description: "Stream torrents on iPhone and iPad directly in your browser. No apps required. Web-based torrent streaming for iOS with cloud processing.",
	},
	{
		Url:         "online-torrent-downloader",
		Title:       "Online Torrent Downloader",
		Benefit:     "Download Torrents Online – No Torrent Client Needed",
		Description: "Download torrent files and magnet links directly in your browser. No torrent client required. Web-based torrent downloading with server-side processing.",
	},
}
