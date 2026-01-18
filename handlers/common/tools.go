package common

// Tool represents a tool page with its URL and display title
type Tool struct {
	Url   string
	Title string
}

// Tools contains the list of all available tool pages
var Tools = []Tool{
	{Url: "torrent-to-ddl", Title: "Torrent → DDL"},
	{Url: "torrent-to-zip", Title: "Torrent → ZIP"},
	{Url: "magnet-to-ddl", Title: "Magnet → DDL"},
	{Url: "magnet-to-torrent", Title: "Magnet → Torrent"},
	{Url: "watch-torrents-online", Title: "Watch Torrents Online"},
	{Url: "watch-torrents-ios", Title: "Watch Torrents on iOS"},
}
