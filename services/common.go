package services

import (
	"regexp"

	"github.com/urfave/cli"
)

var SHA1R = regexp.MustCompile("(?i)[0-9a-f]{5,40}")

var (
	DomainFlag      = "domain"
	DemoMagnetFlag  = "demo-magnet"
	DemoTorrentFlag = "demo-torrent"
	SMTPHostFlag    = "smtp-host"
	SMTPUserFlag    = "smtp-user"
	SMTPPassFlag    = "smtp-pass"
	SMTPPortFlag    = "smtp-port"
	SMTPSecureFlag  = "smtp-secure"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	f = append(f,
		cli.StringFlag{
			Name:   DomainFlag,
			Usage:  "domain",
			Value:  "http://localhost:8080",
			EnvVar: "DOMAIN",
		},
		cli.StringFlag{
			Name:   DemoMagnetFlag,
			Usage:  "demo magnet",
			Value:  "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10",
			EnvVar: "DEMO_MAGNET",
		},
		cli.StringFlag{
			Name:   DemoTorrentFlag,
			Usage:  "demo torrent",
			Value:  "https://webtorrent.io/torrents/sintel.torrent",
			EnvVar: "DEMO_TORRENT",
		},
		cli.StringFlag{
			Name:   SMTPHostFlag,
			Usage:  "smtp host",
			EnvVar: "SMTP_HOST",
		},
		cli.StringFlag{
			Name:   SMTPUserFlag,
			Usage:  "smtp user",
			EnvVar: "SMTP_USER",
		},
		cli.StringFlag{
			Name:   SMTPPassFlag,
			Usage:  "smtp pass",
			EnvVar: "SMTP_PASS",
		},
		cli.IntFlag{
			Name:   SMTPPortFlag,
			Usage:  "smtp port",
			EnvVar: "SMTP_PORT",
			Value:  465,
		},
		cli.BoolTFlag{
			Name:   SMTPSecureFlag,
			Usage:  "smtp secure",
			EnvVar: "SMTP_SECURE",
		},
	)

	return f
}

const AccessTokenParamName = "token"
