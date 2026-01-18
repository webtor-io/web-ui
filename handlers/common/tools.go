package common

// Tool represents a tool page with its URL and display title
type Tool struct {
	Url     string
	Title   string
	Benefit string
}

// Tools contains the list of all available tool pages
var Tools = []Tool{
	{Url: "torrent-to-ddl", Title: "Torrent → DDL", Benefit: "Convert Torrent to Direct Download Link"},
	{Url: "torrent-to-zip", Title: "Torrent → ZIP", Benefit: "Download Torrent Files as ZIP Archive"},
	{Url: "magnet-to-ddl", Title: "Magnet → DDL", Benefit: "Convert Magnet Link to Direct Download Link"},
	{Url: "magnet-to-torrent", Title: "Magnet → Torrent", Benefit: "Convert Magnet Link to .Torrent File"},
	{Url: "watch-torrents-online", Title: "Watch Torrents Online", Benefit: "Stream Torrent Videos in Browser"},
	{Url: "watch-torrents-ios", Title: "Watch Torrents on iOS", Benefit: "Stream Torrent to on iPhone / iPad – No Apps Needed"},
}
