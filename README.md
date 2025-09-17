# web-ui

Some features to mention:

1. Lightweight - less JavaScript code, no frontend frameworks, fewer bytes sent to the client.
2. Based on [webtor REST-API](https://github.com/webtor-io/rest-api).

## Roadmap

- [x] Torrent/magnet upload
- [x] Torrent listing
- [x] Direct file download
- [x] Direct folder download as ZIP-archive
- [x] Picture preview
- [x] Audio streaming
- [x] Video streaming
  - [x] Base player
  - [x] Subtitles support
  - [x] OpenSubtitles support
  - [x] Chromecast support
  - [x] Embed control
  - [ ] Subtitle uploading support
  - [ ] Subtitle size control
- [x] Authentication
  - [x] Passwordless authentication
  - [x] Patreon account linking
  - [x] Google authentication
  - [x] Patreon authentication
- [x] Ads and statistic integration support
- [x] Tools
  - [x] Torrent => DDL
  - [x] Magnet => DDL
  - [x] Magnet => Torrent
  - [x] Torrent => ZIP
- [x] Misc
  - [x] Feedback form
  - [x] Allow magnet-url as query string
  - [x] Add dark/light theme switch
- [x] Chrome extension integration
- [x] Embed support
  - [x] Base version
  - [x] Extended version
- [x] Library
  - [x] Torrents
  - [x] Movies
  - [x] Series
- [x] Content enrichment
    - [x] OMDB
    - [x] Kinopoisk unofficial
    - [ ] AI-powered
- [x] Integrations 
  - [x] Stremio
  - [x] Stremio Addons
  - [x] WebDAV
  - [ ] Torznab

## Setting up connection to Webtor RestAPI

You have to set up connection to [Webtor RestAPI](https://github.com/webtor-io/rest-api) before using WebUI.

If you have already installed [backend part](https://github.com/webtor-io/helm-charts),
then you have to proxy rest-api from your k8s instance to your local machine with [kubefwd](https://github.com/txn2/kubefwd):
```
sudo kubefwd svc -f metadata.name=rest-api -n webtor
```
or with [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl):
```shell
kubectl port-forward svc/rest-api 9090:80 -n webtor

# you have to setup additional environment variables before starting application
export REST_API_SERVICE_PORT=9090
export REST_API_SERVICE_HOST=localhost
```

If you have [RapidAPI subscription](https://rapidapi.com/paveltatarsky-Dx4aX7s_XBt/api/webtor/)
you can just do the following:

```shell
export RAPIDAPI_KEY={YOUR_RAPIDAPI_KEY}
export RAPIDAPI_HOST={YOUR_RAPIDAPI_HOST}
```

## Usage

```shell
./web-ui help
NAME:
   web-ui - runs webtor web ui v2

USAGE:
   web-ui [global options] command [command options] [arguments...]

VERSION:
   0.0.1

COMMANDS:
   serve, s  Serves web server
   help, h   Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h     show help
   --version, -v  print the version
```

## Development

```
npm install
npm start
```

## Building

```shell
make build
```