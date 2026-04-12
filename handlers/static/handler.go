package static

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
)

const (
	AssetsPathFlag = "assets-path"
	AssetsHostFlag = "assets-host"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   AssetsPathFlag,
			Usage:  "assets path",
			Value:  "./assets/dist",
			EnvVar: "ASSETS_PATH",
		},
		cli.StringFlag{
			Name:   AssetsHostFlag,
			Usage:  "assets host",
			Value:  "",
			EnvVar: "WEB_ASSETS_HOST",
		},
	)
}

func RegisterHandler(c *cli.Context, r *gin.Engine) error {
	assetsPath := c.String(AssetsPathFlag)
	pubPath := "pub"

	r.Static("/assets", assetsPath)
	r.Static("/pub", pubPath)

	err := filepath.Walk(pubPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			r.StaticFile(strings.TrimPrefix(path, pubPath), path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Serve favicon and manifest files from root for search engine discoverability
	// (robots.txt blocks /assets/, so root proxying is needed)
	nightPath := assetsPath + "/night"
	for _, name := range []string{
		"favicon.ico",
		"favicon.svg",
		"favicon-16x16.png",
		"favicon-32x32.png",
		"favicon-48x48.png",
		"manifest.webmanifest",
		"android-chrome-36x36.png",
		"android-chrome-48x48.png",
		"android-chrome-72x72.png",
		"android-chrome-96x96.png",
		"android-chrome-144x144.png",
		"android-chrome-192x192.png",
		"android-chrome-256x256.png",
		"android-chrome-384x384.png",
		"android-chrome-512x512.png",
	} {
		r.StaticFile("/"+name, nightPath+"/"+name)
	}
	return nil
}
