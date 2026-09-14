# subtitle-translate: сервис и деплой — план имплементации (план A фазы 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Новый Go-сервис `subtitle-translate`, который по цепочке THP `~tr:<lang>` переводит WebVTT на язык зрителя батчами с прогрессивной выдачей, и его деплой в прод. Web-ui сюда не входит (план B).

**Architecture:** Сервис по образцу srt2vtt: один HTTP-маршрут, `X-Source-Url` от THP как источник, целевой язык из собственного пути запроса. Перевод батчами через интерфейс `Translator` (реализация на anthropic-sdk-go, модель флагом), ход работы в Redis по батчам, готовый VTT в S3 (свой бакет `subtitle-translate`, один бакет на сервис), один перевод на ключ (singleflight в поде + замок в Redis). Первый GET запускает задачу и отвечает тем, что готово; HEAD только отдаёт прогресс.

**Tech Stack:** Go 1.23, urfave/cli v1, `common-services` (probe, serve, Redis go-redis v9, S3 aws-sdk-go v1, prom), `go-astisub v0.32.0`, `anthropic-sdk-go v1.34.0`, logrus, pkg/errors, prometheus/client_golang. Deploy: helm chart + helmfile + `sync.sh`, образ через reusable workflow `webtor-io/.github`.

**Spec:** `web-ui/docs/superpowers/specs/2026-09-13-subtitle-translate-design.md` (разделы «Сервис `subtitle-translate`», «Цепочка URL», «THP», «Гейтинг по возможностям», «Тестирование»).

## Global Constraints

- Маршрут: `GET|HEAD /<anything>~tr:<lang>/<name>.vtt`; язык это 2-буквенный код из списка сервиса, иначе 400. `HEAD` задачу не запускает.
- Ключ артефакта: `k = hex(sha256(X-Info-Hash + X-Path + lang + model + promptVersion))`.
- Ответ: `Content-Type: text/vtt; charset=utf-8`; `X-Subtitle-Progress: <готово>/<всего>`; `Cache-Control: no-store` пока не завершено, `public, max-age=86400` после; тело — `WEBVTT` плюс переведённые cues по порядку с начала.
- Коды: нет ключа API → 501 `translation is not configured`; плохой язык → 400; источник недоступен → 404 с причиной; больше `--max-source-bytes` (1 МиБ) или `--max-cues` (5000) → 413; ошибка модели после ретраев → 502.
- Батч: `--batch-size` (50) реплик; контекст = последние 5 реплик предыдущего батча; глоссарий из query `names=` (до 30 имён); температура 0; несовпадение числа строк → один повтор, затем оригинальные реплики без перевода и метрика.
- Нормализация: удалить HI-теги `[…]` и `(…)`, строки только из `♪`/`#`/пробелов, cue, ставшие пустыми, остаются пустыми (тайминги сохраняются).
- Тайминги, порядок и число cue не меняются; переносы внутри cue сохраняются.
- Redis: `tr:cues:<k>` (gob `[]string`, TTL 24h), `tr:total:<k>`, замок `tr:lock:<k>` (SETNX, 10 мин, продлевается на каждом батче). S3: бакет `subtitle-translate`, ключ `<k>.vtt`, `ContentType: text/vtt; charset=utf-8`, бессрочно. После записи в S3 Redis-ключи удаляются.
- В логах нет текста реплик. В ошибках наружу «upstream», имя провайдера только во флагах и README.
- Порты: 8080 HTTP, 8081 probe, 8083 Prometheus.
- Dockerfile трёхстадийный с `ca-certificates` (образец `video-info/Dockerfile`), иначе TLS к API не работает.
- Релиз/образ/ключ в `images.yaml`/репозиторий GitHub — одно имя `subtitle-translate`.
- Коммиты с трейлером:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01VwLULKCA6QwcXFJVLFrqJ9
  ```

---

## Карта файлов (репозиторий `subtitle-translate`, локально `/Users/vintikzzzz/Projects/webtor/subtitle-translate`)

| Файл | Ответственность |
|---|---|
| `main.go` | cli app, `configure(app)`, запуск |
| `configure.go` | регистрация флагов, сборка зависимостей, `cs.NewServe` |
| `services/langs.go` | список поддерживаемых языков и их английские названия для промпта |
| `services/vtt.go` | парсинг/сериализация VTT, нормализация, нарезка на батчи, частичная выдача |
| `services/translator.go` | интерфейс `Translator`, `BatchRequest`, парсинг ответа «N: text» |
| `services/prompt.go` | текст промпта и `PromptVersion` |
| `services/anthropic.go` | реализация `Translator` на SDK |
| `services/store.go` | интерфейс `Store` (ход работы + финал), `MemoryStore` |
| `services/redis_store.go` | `RedisStore` поверх go-redis + S3 |
| `services/job.go` | `Runner`: ключ артефакта, singleflight, фоновая задача, прогресс |
| `services/web.go` | HTTP: разбор пути, источник, лимиты, коды, заголовки |
| `services/metrics.go` | Prometheus-счётчики |
| `Dockerfile`, `.github/workflows/docker-image.yml`, `.github/FUNDING.yml`, `LICENSE`, `.gitignore`, `README.md` | инфраструктура репозитория |

Инфра (`/Users/vintikzzzz/Projects/webtor/infra/helmfile`): `charts/subtitle-translate/*`, `values/subtitle-translate.yaml.gotmpl`, `helmfile.yaml` (+5 строк), `values/torrent-http-proxy/services.yaml` (+2 строки), `environments/default/images.yaml` (+1 строка).

---

### Task 1: Скелет репозитория и HTTP-каркас с 501 без ключа

**Files:**
- Create: `main.go`, `configure.go`, `services/web.go`, `services/metrics.go`, `go.mod`, `Dockerfile`, `.github/workflows/docker-image.yml`, `.github/FUNDING.yml`, `LICENSE`, `.gitignore`, `README.md`
- Test: `services/web_test.go`

**Interfaces:**
- Produces: `type Web struct`, `func NewWeb(c *cli.Context, h http.Handler) *Web` (слушает `--host/--port`, оборачивает handler logrus-middleware), `func RegisterWebFlags(f []cli.Flag) []cli.Flag`; `type Handler struct{ Runner *Runner; ... }` появится в Task 7, здесь временный `func NotConfiguredHandler() http.Handler`, отвечающий 501 на всё.
- Produces: `metrics.go`: `var (BatchesTotal, TokensInput, TokensOutput prometheus.Counter; JobErrors *prometheus.CounterVec (label code); JobDuration prometheus.Histogram)`, зарегистрированные в `init()`.

- [ ] **Step 1: Создать репозиторий и модуль**

```bash
mkdir -p /Users/vintikzzzz/Projects/webtor/subtitle-translate && cd $_ && git init -q -b master
cat > go.mod <<'EOF'
module github.com/webtor-io/subtitle-translate

go 1.23
EOF
go get github.com/urfave/cli@v1.22.16 github.com/sirupsen/logrus@v1.9.3 github.com/pkg/errors@v0.9.1 \
  github.com/bakins/logrus-middleware@v0.0.0-20180426214643-ce4c6f8deb07 \
  github.com/webtor-io/common-services@v0.0.0-20250112153432-554128b56bd5 \
  github.com/asticode/go-astisub@v0.32.0 github.com/anthropics/anthropic-sdk-go@v1.34.0 \
  github.com/prometheus/client_golang@latest
```

`.gitignore`:
```
subtitle-translate
server
tmp
chart
subtitle-translate.yaml.gotmpl
.idea
```

`LICENSE`: MIT, `Copyright (c) 2026 webtor.io` (текст взять из `/Users/vintikzzzz/Projects/webtor/srt2vtt/LICENSE`, заменив год). `.github/FUNDING.yml`: скопировать из srt2vtt. `.github/workflows/docker-image.yml`:

```yaml
name: Docker Image CI

on:
  workflow_dispatch:
  push:
    branches:
      - 'master'
    tags:
      - 'v*'

jobs:
  docker:
    uses: webtor-io/.github/.github/workflows/docker-multiarch.yml@main
    permissions:
      contents: read
      packages: write
```

`Dockerfile` (трёхстадийный, по `video-info/Dockerfile`):
```dockerfile
FROM alpine:3.21 AS certs
RUN apk add --no-cache ca-certificates

FROM golang:1.23-alpine3.21 AS build
WORKDIR /app
COPY . .
ENV GOOS=linux CGO_ENABLED=0
RUN go build -ldflags '-w -s' -o server

FROM alpine:3.21
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /app/server .
EXPOSE 8080 8081 8083
CMD ["./server"]
```

- [ ] **Step 2: Падающий тест каркаса**

```go
// services/web_test.go
package services

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotConfiguredHandlerReturns501(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/abc/movie.srt~vtt/movie.vtt~tr:pt/movie.vtt", nil)
	NotConfiguredHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("code=%d", rec.Code)
	}
	if got := rec.Body.String(); got != "translation is not configured\n" {
		t.Fatalf("body=%q", got)
	}
}
```

Run: `go test ./services/ -run TestNotConfigured -v` → FAIL `undefined: NotConfiguredHandler`.

- [ ] **Step 3: Реализация**

```go
// services/web.go
package services

import (
	"fmt"
	"net"
	"net/http"

	logrusmiddleware "github.com/bakins/logrus-middleware"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	webHostFlag = "host"
	webPortFlag = "port"
)

func RegisterWebFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{Name: webHostFlag, Usage: "listening host", Value: "", EnvVar: "WEB_HOST"},
		cli.IntFlag{Name: webPortFlag, Usage: "http listening port", Value: 8080, EnvVar: "WEB_PORT"},
	)
}

// NotConfiguredHandler is served when no API key is configured: the
// capability is absent and says so instead of failing silently.
func NotConfiguredHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "translation is not configured", http.StatusNotImplemented)
	})
}

type Web struct {
	host string
	port int
	h    http.Handler
	ln   net.Listener
}

func NewWeb(c *cli.Context, h http.Handler) *Web {
	return &Web{host: c.String(webHostFlag), port: c.Int(webPortFlag), h: h}
}

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to listen to tcp connection")
	}
	s.ln = ln
	logger := log.New()
	m := logrusmiddleware.Middleware{Logger: logger}
	srv := &http.Server{Handler: m.Handler(s.h, ""), MaxHeaderBytes: 50 << 20}
	log.Infof("serving web at %v", addr)
	return srv.Serve(ln)
}

func (s *Web) Close() {
	if s.ln != nil {
		_ = s.ln.Close()
	}
}
```

```go
// services/metrics.go
package services

import "github.com/prometheus/client_golang/prometheus"

var (
	BatchesTotal = prometheus.NewCounter(prometheus.CounterOpts{Name: "subtitle_translate_batches_total", Help: "translated batches"})
	TokensInput  = prometheus.NewCounter(prometheus.CounterOpts{Name: "subtitle_translate_tokens_input_total", Help: "upstream input tokens"})
	TokensOutput = prometheus.NewCounter(prometheus.CounterOpts{Name: "subtitle_translate_tokens_output_total", Help: "upstream output tokens"})
	JobErrors    = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "subtitle_translate_job_errors_total", Help: "job errors by code"}, []string{"code"})
	JobDuration  = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "subtitle_translate_job_seconds", Help: "job duration", Buckets: []float64{5, 15, 30, 60, 120, 300, 600}})
	LineMismatch = prometheus.NewCounter(prometheus.CounterOpts{Name: "subtitle_translate_line_mismatch_total", Help: "batches whose reply line count did not match"})
)

func init() {
	prometheus.MustRegister(BatchesTotal, TokensInput, TokensOutput, JobErrors, JobDuration, LineMismatch)
}
```

```go
// main.go
package main

import (
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

func main() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	app := cli.NewApp()
	app.Name = "subtitle-translate"
	app.Usage = "translates WebVTT subtitles into the viewer's language"
	app.Version = "0.1.0"
	configure(app)
	if err := app.Run(os.Args); err != nil {
		log.WithError(err).Fatal("failed to serve application")
	}
}
```

```go
// configure.go (первая версия; Task 7 заменит NotConfiguredHandler на настоящий handler)
package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/subtitle-translate/services"
)

func configure(app *cli.App) {
	app.Flags = []cli.Flag{}
	app.Flags = cs.RegisterProbeFlags(app.Flags)
	app.Flags = cs.RegisterPromFlags(app.Flags)
	app.Flags = services.RegisterWebFlags(app.Flags)
	app.Action = run
}

func run(c *cli.Context) error {
	var servers []cs.Servable
	if probe := cs.NewProbe(c); probe != nil {
		servers = append(servers, probe)
		defer probe.Close()
	}
	if prom := cs.NewProm(c); prom != nil {
		servers = append(servers, prom)
		defer prom.Close()
	}
	web := services.NewWeb(c, services.NotConfiguredHandler())
	servers = append(servers, web)
	defer web.Close()
	if err := cs.NewServe(servers...).Serve(); err != nil {
		log.WithError(err).Error("got serve error")
		return err
	}
	return nil
}
```

`README.md` первая версия: заголовок, одна строка «Translates WebVTT subtitles into the viewer's language; reached through torrent-http-proxy as the `~tr:<lang>` mod», раздел Usage дополняется в Task 8.

- [ ] **Step 4: Тесты и сборка**

Run: `go mod tidy && go build ./... && go vet ./... && go test ./... -v` → PASS.

- [ ] **Step 5: Коммит**

```bash
git add go.mod go.sum main.go configure.go services/web.go services/web_test.go services/metrics.go Dockerfile .github .gitignore LICENSE README.md
git commit -m "scaffold: cli app, web server, 501 without an API key"
```

---

### Task 2: Языки и модель VTT (парсинг, нормализация, батчи, частичная выдача)

**Files:**
- Create: `services/langs.go`, `services/vtt.go`
- Test: `services/vtt_test.go`, `services/langs_test.go`

**Interfaces:**
- Produces: `func LangName(code string) (string, bool)` — английское название языка по 2-буквенному коду (`"pt"` → `"Portuguese"`), `false` для неизвестного.
- Produces: `type Cue struct{ Index int; Start, End time.Duration; Lines []string }`; `type Doc struct{ Items *astisub.Subtitles; Cues []Cue }`; `func ParseVTT(r io.Reader) (*Doc, error)`; `func (d *Doc) Normalize()` (мутирует `Cues`); `func Batches(n, size int) [][2]int` (полуинтервалы `[from,to)`); `func (d *Doc) Render(translated []string, upTo int) ([]byte, error)` — VTT с первыми `upTo` cue, где для cue `i` текст берётся из `translated[i]` (переносы `\n`), при пустом `translated[i]` берётся оригинал; `func JoinLines(c Cue) string` / `func SplitLines(s string) []string` для обмена с моделью (перенос кодируется как ` ⏎ `).

- [ ] **Step 1: Падающие тесты**

```go
// services/langs_test.go
package services

import "testing"

func TestLangName(t *testing.T) {
	for code, want := range map[string]string{"pt": "Portuguese", "ru": "Russian", "en": "English", "zh": "Chinese"} {
		if got, ok := LangName(code); !ok || got != want {
			t.Errorf("%s: got %q ok=%v", code, got, ok)
		}
	}
	for _, bad := range []string{"", "xx", "PT", "pt-BR", "eng"} {
		if _, ok := LangName(bad); ok {
			t.Errorf("%q must be unknown", bad)
		}
	}
}
```

```go
// services/vtt_test.go
package services

import (
	"strings"
	"testing"
)

const sampleVTT = `WEBVTT

1
00:00:01.000 --> 00:00:02.000
[DOOR SLAMS]

2
00:00:03.000 --> 00:00:04.000
Hello there,
General Kenobi.

3
00:00:05.000 --> 00:00:06.000
♪ ♪

4
00:00:07.000 --> 00:00:08.000
(sighs) Fine. [laughs] Let's go.
`

func TestParseAndNormalize(t *testing.T) {
	d, err := ParseVTT(strings.NewReader(sampleVTT))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Cues) != 4 {
		t.Fatalf("cues=%d", len(d.Cues))
	}
	d.Normalize()
	got := []string{JoinLines(d.Cues[0]), JoinLines(d.Cues[1]), JoinLines(d.Cues[2]), JoinLines(d.Cues[3])}
	want := []string{"", "Hello there, ⏎ General Kenobi.", "", "Fine. Let's go."}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cue %d: got %q want %q", i, got[i], want[i])
		}
	}
	if d.Cues[1].Start.Seconds() != 3 || d.Cues[1].End.Seconds() != 4 {
		t.Errorf("timings changed: %v-%v", d.Cues[1].Start, d.Cues[1].End)
	}
}

func TestBatches(t *testing.T) {
	got := Batches(7, 3)
	want := [][2]int{{0, 3}, {3, 6}, {6, 7}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if len(Batches(0, 3)) != 0 {
		t.Fatal("empty input must give no batches")
	}
}

func TestRenderPartialKeepsTimingsAndOrder(t *testing.T) {
	d, _ := ParseVTT(strings.NewReader(sampleVTT))
	d.Normalize()
	tr := []string{"", "Olá, ⏎ General Kenobi.", "", ""}
	out, err := d.Render(tr, 2)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "WEBVTT") {
		t.Fatalf("no header: %q", s[:20])
	}
	if !strings.Contains(s, "00:00:03.000 --> 00:00:04.000\nOlá,\nGeneral Kenobi.") {
		t.Fatalf("translated cue with line break missing:\n%s", s)
	}
	if strings.Contains(s, "00:00:05.000") {
		t.Fatalf("cue beyond upTo rendered:\n%s", s)
	}
	full, _ := d.Render(tr, 4)
	if !strings.Contains(string(full), "Fine. Let's go.") {
		t.Fatalf("untranslated cue must fall back to the original:\n%s", full)
	}
}

func TestSplitJoinRoundTrip(t *testing.T) {
	c := Cue{Lines: []string{"a", "b"}}
	if got := SplitLines(JoinLines(c)); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}
```

Run: `go test ./services/ -run 'TestLangName|TestParseAndNormalize|TestBatches|TestRender|TestSplitJoin' -v` → FAIL, undefined.

- [ ] **Step 2: Реализация**

```go
// services/langs.go
package services

// langNames maps the 2-letter codes the service accepts to the English
// language name used in the prompt. Kept broader than the 11 UI locales:
// the preferred language comes from the profile setting.
var langNames = map[string]string{
	"en": "English", "ru": "Russian", "es": "Spanish", "de": "German", "fr": "French",
	"pt": "Portuguese", "it": "Italian", "pl": "Polish", "tr": "Turkish", "nl": "Dutch",
	"cs": "Czech", "uk": "Ukrainian", "zh": "Chinese", "ja": "Japanese", "ko": "Korean",
	"ar": "Arabic", "hi": "Hindi", "id": "Indonesian", "vi": "Vietnamese", "th": "Thai",
	"sv": "Swedish", "no": "Norwegian", "da": "Danish", "fi": "Finnish", "el": "Greek",
	"he": "Hebrew", "hu": "Hungarian", "ro": "Romanian", "bg": "Bulgarian", "sr": "Serbian",
	"hr": "Croatian", "sk": "Slovak", "sl": "Slovenian", "lt": "Lithuanian", "lv": "Latvian",
	"et": "Estonian", "fa": "Persian", "ms": "Malay", "bn": "Bengali", "ta": "Tamil",
	"kk": "Kazakh", "ka": "Georgian", "hy": "Armenian", "az": "Azerbaijani", "ca": "Catalan",
}

func LangName(code string) (string, bool) {
	n, ok := langNames[code]
	return n, ok
}
```

```go
// services/vtt.go
package services

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/asticode/go-astisub"
	"github.com/pkg/errors"
)

const lineBreakToken = " ⏎ "

type Cue struct {
	Index int
	Start time.Duration
	End   time.Duration
	Lines []string
}

type Doc struct {
	Items *astisub.Subtitles
	Cues  []Cue
}

func ParseVTT(r io.Reader) (*Doc, error) {
	subs, err := astisub.ReadFromWebVTT(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse webvtt")
	}
	d := &Doc{Items: subs}
	for i, it := range subs.Items {
		c := Cue{Index: i, Start: it.StartAt, End: it.EndAt}
		for _, l := range it.Lines {
			c.Lines = append(c.Lines, l.String())
		}
		d.Cues = append(d.Cues, c)
	}
	return d, nil
}

var (
	hiTagRe     = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)
	musicOnlyRe = regexp.MustCompile(`^[\s♪#♫]*$`)
	spacesRe    = regexp.MustCompile(`\s{2,}`)
)

// Normalize strips hearing-impaired markup and music-only lines so the
// model translates dialogue only. Timings and cue count are untouched: a
// cue that becomes empty stays as an empty cue.
func (d *Doc) Normalize() {
	for ci := range d.Cues {
		var kept []string
		for _, l := range d.Cues[ci].Lines {
			l = hiTagRe.ReplaceAllString(l, "")
			l = strings.TrimSpace(spacesRe.ReplaceAllString(l, " "))
			if l == "" || musicOnlyRe.MatchString(l) {
				continue
			}
			kept = append(kept, l)
		}
		d.Cues[ci].Lines = kept
	}
}

func Batches(n, size int) [][2]int {
	if size <= 0 {
		size = 50
	}
	var out [][2]int
	for from := 0; from < n; from += size {
		to := from + size
		if to > n {
			to = n
		}
		out = append(out, [2]int{from, to})
	}
	return out
}

func JoinLines(c Cue) string { return strings.Join(c.Lines, lineBreakToken) }

func SplitLines(s string) []string {
	var out []string
	for _, p := range strings.Split(s, strings.TrimSpace(lineBreakToken)) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Render writes a WebVTT with the first upTo cues. translated[i] replaces
// cue i's text when non-empty; otherwise the original lines are kept.
func (d *Doc) Render(translated []string, upTo int) ([]byte, error) {
	if upTo > len(d.Cues) {
		upTo = len(d.Cues)
	}
	out := &astisub.Subtitles{Metadata: d.Items.Metadata}
	for i := 0; i < upTo; i++ {
		src := d.Items.Items[i]
		lines := src.Lines
		if i < len(translated) && strings.TrimSpace(translated[i]) != "" {
			lines = nil
			for _, t := range SplitLines(translated[i]) {
				lines = append(lines, astisub.Line{Items: []astisub.LineItem{{Text: t}}})
			}
		}
		out.Items = append(out.Items, &astisub.Item{StartAt: src.StartAt, EndAt: src.EndAt, Lines: lines})
	}
	buf := &bytes.Buffer{}
	if len(out.Items) == 0 {
		buf.WriteString("WEBVTT\n\n")
		return buf.Bytes(), nil
	}
	if err := out.WriteToWebVTT(buf); err != nil {
		return nil, errors.Wrap(err, "failed to write webvtt")
	}
	return buf.Bytes(), nil
}
```

Примечание для реализатора: `astisub` при чтении WebVTT теряет нецифровые идентификаторы cue и при записи нумерует их заново `1..N`; на это никто не опирается, cue адресуются позицией. Если `Render` c `upTo=2` для cue 1 у `Hello there, / General Kenobi.` не даёт двух строк, проверить `SplitLines` по токену без пробелов.

- [ ] **Step 3: Тесты**

Run: `go test ./services/ -v` → PASS. Негативный контроль: убрать `musicOnlyRe`-ветку в `Normalize` — `TestParseAndNormalize` красный на cue 2. Вернуть.

- [ ] **Step 4: Коммит**

```bash
git add services/langs.go services/langs_test.go services/vtt.go services/vtt_test.go go.mod go.sum
git commit -m "vtt: parse, normalize HI markup, batch, partial render"
```

---

### Task 3: Интерфейс `Translator`, промпт, парсинг ответа

**Files:**
- Create: `services/translator.go`, `services/prompt.go`
- Test: `services/translator_test.go`

**Interfaces:**
- Produces: `type BatchRequest struct{ TargetLang, TargetName, SourceLang string; Context []string; Glossary []string; Lines []string }`; `type BatchResult struct{ Lines []string; InputTokens, OutputTokens int64 }`; `type Translator interface{ Translate(ctx context.Context, req BatchRequest) (BatchResult, error) }`.
- Produces: `const PromptVersion = "v1"`; `func BuildSystemPrompt(targetName string) string`; `func BuildUserPrompt(req BatchRequest) string` (нумерует строки с 1); `func ParseReply(reply string, n int) ([]string, error)` — ждёт ровно `n` строк вида `<i>: <text>`, иначе `ErrLineMismatch`.

- [ ] **Step 1: Падающие тесты**

```go
// services/translator_test.go
package services

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildUserPromptNumbersLinesAndCarriesContext(t *testing.T) {
	p := BuildUserPrompt(BatchRequest{
		TargetName: "Portuguese", SourceLang: "en",
		Context:  []string{"Previous line."},
		Glossary: []string{"Hildy", "Walter"},
		Lines:    []string{"Hello there, ⏎ General Kenobi.", "Fine."},
	})
	for _, want := range []string{"1: Hello there, ⏎ General Kenobi.", "2: Fine.", "Previous line.", "Hildy", "Walter", "Portuguese"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
}

func TestParseReply(t *testing.T) {
	got, err := ParseReply("1: Olá, ⏎ General Kenobi.\n2: Tudo bem.\n", 2)
	if err != nil || len(got) != 2 || got[0] != "Olá, ⏎ General Kenobi." || got[1] != "Tudo bem." {
		t.Fatalf("got %v err %v", got, err)
	}
	// tolerated noise: blank lines, code fences, numbering with a dot
	got, err = ParseReply("```\n1. Olá\n\n2. Tudo bem\n```", 2)
	if err != nil || got[1] != "Tudo bem" {
		t.Fatalf("got %v err %v", got, err)
	}
	if _, err := ParseReply("1: only one", 2); !errors.Is(err, ErrLineMismatch) {
		t.Fatalf("expected ErrLineMismatch, got %v", err)
	}
	if _, err := ParseReply("1: a\n3: b", 2); !errors.Is(err, ErrLineMismatch) {
		t.Fatalf("wrong numbering must be a mismatch, got %v", err)
	}
}
```

Run: `go test ./services/ -run 'TestBuildUserPrompt|TestParseReply' -v` → FAIL, undefined.

- [ ] **Step 2: Реализация**

```go
// services/prompt.go
package services

import (
	"fmt"
	"strings"
)

// PromptVersion is part of the artifact key: changing the prompt
// invalidates cached translations.
const PromptVersion = "v1"

func BuildSystemPrompt(targetName string) string {
	return fmt.Sprintf(`You translate film and TV subtitles into %s.
Rules:
- Translate every numbered line; output exactly the same number of lines, each as "<number>: <translation>", nothing else.
- Keep the " ⏎ " token where a line break belongs; do not add or remove tokens.
- Keep names from the glossary unchanged unless the language requires inflection.
- Preserve tone, register and profanity; do not soften, explain or add notes.
- Lines are consecutive dialogue; use the previous lines for context but do not translate them.
- If a line is not translatable (a name, a number), copy it as is.`, targetName)
}

func BuildUserPrompt(req BatchRequest) string {
	var b strings.Builder
	if req.SourceLang != "" {
		fmt.Fprintf(&b, "Source language: %s\n", req.SourceLang)
	}
	if len(req.Glossary) > 0 {
		fmt.Fprintf(&b, "Glossary (character names): %s\n", strings.Join(req.Glossary, ", "))
	}
	if len(req.Context) > 0 {
		b.WriteString("Previous lines (context only):\n")
		for _, c := range req.Context {
			b.WriteString("- " + c + "\n")
		}
	}
	fmt.Fprintf(&b, "Translate into %s:\n", req.TargetName)
	for i, l := range req.Lines {
		fmt.Fprintf(&b, "%d: %s\n", i+1, l)
	}
	return b.String()
}
```

```go
// services/translator.go
package services

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

type BatchRequest struct {
	TargetLang string
	TargetName string
	SourceLang string
	Context    []string
	Glossary   []string
	Lines      []string
}

type BatchResult struct {
	Lines        []string
	InputTokens  int64
	OutputTokens int64
}

type Translator interface {
	Translate(ctx context.Context, req BatchRequest) (BatchResult, error)
}

var ErrLineMismatch = errors.New("reply line count does not match the request")

var replyLineRe = regexp.MustCompile(`^\s*(\d+)\s*[:.)]\s*(.*)$`)

// ParseReply accepts "<i>: <text>" lines (also "<i>." and "<i>)"), ignores
// blank lines and code fences, and requires exactly n lines numbered 1..n.
func ParseReply(reply string, n int) ([]string, error) {
	out := make([]string, 0, n)
	for _, raw := range strings.Split(reply, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		m := replyLineRe.FindStringSubmatch(line)
		if m == nil {
			return nil, errors.Wrapf(ErrLineMismatch, "unnumbered line %q", line)
		}
		idx, _ := strconv.Atoi(m[1])
		if idx != len(out)+1 {
			return nil, errors.Wrapf(ErrLineMismatch, "expected line %d, got %d", len(out)+1, idx)
		}
		out = append(out, strings.TrimSpace(m[2]))
	}
	if len(out) != n {
		return nil, errors.Wrapf(ErrLineMismatch, "expected %d lines, got %d", n, len(out))
	}
	return out, nil
}
```

- [ ] **Step 3: Тесты** — `go test ./services/ -v` → PASS.

- [ ] **Step 4: Коммит**

```bash
git add services/translator.go services/prompt.go services/translator_test.go
git commit -m "translator: batch contract, prompt v1, strict reply parsing"
```

---

### Task 4: Реализация `Translator` на SDK

**Files:**
- Create: `services/anthropic.go`
- Test: `services/anthropic_test.go`

**Interfaces:**
- Consumes: `BatchRequest`, `BatchResult`, `ParseReply`, `BuildSystemPrompt`, `BuildUserPrompt`, `ErrLineMismatch`, метрики `TokensInput/TokensOutput/LineMismatch`.
- Produces: `func RegisterTranslatorFlags(f []cli.Flag) []cli.Flag` (`--anthropic-api-key` `ANTHROPIC_API_KEY`; `--model` `SUBTITLE_TRANSLATE_MODEL` default `claude-haiku-4-5-20251001`; `--upstream-timeout` секунды `SUBTITLE_TRANSLATE_UPSTREAM_TIMEOUT` default 60; `--max-tokens` default 4096); `func NewAnthropicTranslator(c *cli.Context, opts ...option.RequestOption) Translator` — возвращает `nil`, когда ключ пуст; `func (t *AnthropicTranslator) Model() string`.
- Поведение: один вызов Messages без tools, температура 0; текст ответа = конкатенация блоков `Type=="text"`; `ParseReply`; при `ErrLineMismatch` один повтор с добавленной строкой «Reminder: exactly N numbered lines.»; после второго промаха возвращается `ErrLineMismatch` (вызывающий подставит оригинал); `LineMismatch.Inc()` на каждый промах; токены в метрики и в `BatchResult`.

- [ ] **Step 1: Падающий тест с фейковым сервером API**

```go
// services/anthropic_test.go
package services

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/urfave/cli"
)

func testCtx(t *testing.T, key string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("t", 0)
	set.String(flagAPIKey, key, "")
	set.String(flagModel, "test-model", "")
	set.Int(flagUpstreamTimeout, 5, "")
	set.Int(flagMaxTokens, 512, "")
	return cli.NewContext(nil, set, nil)
}

func fakeAPI(t *testing.T, replies []string) (*httptest.Server, *int32, *[]map[string]any) {
	t.Helper()
	var n int32
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		bodies = append(bodies, m)
		i := int(atomic.AddInt32(&n, 1)) - 1
		if i >= len(replies) {
			i = len(replies) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg", "type": "message", "role": "assistant", "model": "test-model",
			"content":     []map[string]any{{"type": "text", "text": replies[i]}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 11, "output_tokens": 7},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &n, &bodies
}

func TestNilWithoutKey(t *testing.T) {
	if tr := NewAnthropicTranslator(testCtx(t, "")); tr != nil {
		t.Fatal("expected nil translator without a key")
	}
}

func TestTranslateParsesTextAndUsage(t *testing.T) {
	srv, n, bodies := fakeAPI(t, []string{"1: Olá\n2: Tchau"})
	tr := NewAnthropicTranslator(testCtx(t, "k"), option.WithBaseURL(srv.URL))
	res, err := tr.Translate(context.Background(), BatchRequest{TargetLang: "pt", TargetName: "Portuguese", Lines: []string{"Hi", "Bye"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) != 2 || res.Lines[0] != "Olá" || res.InputTokens != 11 || res.OutputTokens != 7 {
		t.Fatalf("res=%+v", res)
	}
	if *n != 1 {
		t.Fatalf("calls=%d", *n)
	}
	body := (*bodies)[0]
	if body["model"] != "test-model" || body["temperature"] != 0.0 {
		t.Fatalf("body=%v", body)
	}
	if _, has := body["tools"]; has {
		t.Fatal("must be a plain text call, no tools")
	}
}

func TestTranslateRetriesOnceOnMismatch(t *testing.T) {
	srv, n, _ := fakeAPI(t, []string{"1: only", "1: Olá\n2: Tchau"})
	tr := NewAnthropicTranslator(testCtx(t, "k"), option.WithBaseURL(srv.URL))
	res, err := tr.Translate(context.Background(), BatchRequest{TargetLang: "pt", TargetName: "Portuguese", Lines: []string{"Hi", "Bye"}})
	if err != nil || len(res.Lines) != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if *n != 2 {
		t.Fatalf("calls=%d want 2", *n)
	}
}

func TestTranslateGivesUpAfterSecondMismatch(t *testing.T) {
	srv, n, _ := fakeAPI(t, []string{"1: only", "garbage"})
	tr := NewAnthropicTranslator(testCtx(t, "k"), option.WithBaseURL(srv.URL))
	_, err := tr.Translate(context.Background(), BatchRequest{TargetLang: "pt", TargetName: "Portuguese", Lines: []string{"Hi", "Bye"}})
	if !errors.Is(err, ErrLineMismatch) || *n != 2 {
		t.Fatalf("err=%v calls=%d", err, *n)
	}
}
```

Run: `go test ./services/ -run 'TestNilWithoutKey|TestTranslate' -v` → FAIL, undefined.

- [ ] **Step 2: Реализация**

```go
// services/anthropic.go
package services

import (
	"context"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	flagAPIKey          = "anthropic-api-key"
	flagModel           = "model"
	flagUpstreamTimeout = "upstream-timeout"
	flagMaxTokens       = "max-tokens"
)

func RegisterTranslatorFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{Name: flagAPIKey, Usage: "upstream model API key; empty disables translation", EnvVar: "ANTHROPIC_API_KEY"},
		cli.StringFlag{Name: flagModel, Usage: "upstream model id", Value: "claude-haiku-4-5-20251001", EnvVar: "SUBTITLE_TRANSLATE_MODEL"},
		cli.IntFlag{Name: flagUpstreamTimeout, Usage: "per-batch upstream timeout, seconds", Value: 60, EnvVar: "SUBTITLE_TRANSLATE_UPSTREAM_TIMEOUT"},
		cli.IntFlag{Name: flagMaxTokens, Usage: "max output tokens per batch", Value: 4096, EnvVar: "SUBTITLE_TRANSLATE_MAX_TOKENS"},
	)
}

type AnthropicTranslator struct {
	cl        anthropic.Client
	model     string
	timeout   time.Duration
	maxTokens int64
}

// NewAnthropicTranslator returns nil when no key is configured: the
// capability is absent, and the handler answers 501.
func NewAnthropicTranslator(c *cli.Context, opts ...option.RequestOption) Translator {
	key := strings.TrimSpace(c.String(flagAPIKey))
	if key == "" {
		log.Info("no upstream API key: translation disabled")
		return nil
	}
	opts = append([]option.RequestOption{option.WithAPIKey(key)}, opts...)
	return &AnthropicTranslator{
		cl:        anthropic.NewClient(opts...),
		model:     c.String(flagModel),
		timeout:   time.Duration(c.Int(flagUpstreamTimeout)) * time.Second,
		maxTokens: int64(c.Int(flagMaxTokens)),
	}
}

func (t *AnthropicTranslator) Model() string { return t.model }

func (t *AnthropicTranslator) Translate(ctx context.Context, req BatchRequest) (BatchResult, error) {
	var res BatchResult
	user := BuildUserPrompt(req)
	for attempt := 1; attempt <= 2; attempt++ {
		lines, in, out, err := t.call(ctx, BuildSystemPrompt(req.TargetName), user, len(req.Lines))
		res.InputTokens += in
		res.OutputTokens += out
		TokensInput.Add(float64(in))
		TokensOutput.Add(float64(out))
		if err == nil {
			res.Lines = lines
			return res, nil
		}
		if !errors.Is(err, ErrLineMismatch) {
			return res, err
		}
		LineMismatch.Inc()
		user = user + "\nReminder: output exactly " + strconv.Itoa(len(req.Lines)) + " numbered lines, nothing else.\n"
		if attempt == 2 {
			return res, err
		}
	}
	return res, ErrLineMismatch
}

func (t *AnthropicTranslator) call(ctx context.Context, system, user string, n int) ([]string, int64, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	resp, err := t.cl.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       anthropic.Model(t.model),
		MaxTokens:   t.maxTokens,
		System:      []anthropic.TextBlockParam{{Text: system}},
		Messages:    []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(user))},
		Temperature: anthropic.Float(0),
	})
	if err != nil {
		return nil, 0, 0, errors.Wrap(err, "upstream request failed")
	}
	var sb strings.Builder
	for _, b := range resp.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	lines, err := ParseReply(sb.String(), n)
	return lines, resp.Usage.InputTokens, resp.Usage.OutputTokens, err
}
```

Импорты: `context`, `strconv`, `strings`, `time`, SDK и `option`, `pkg/errors`, logrus, `urfave/cli`. Проверить в SDK v1.34.0 имена: `anthropic.Model`, `anthropic.Float`, `anthropic.NewUserMessage`, `anthropic.NewTextBlock`, `resp.Usage.InputTokens` (`int64`), `b.Type`, `b.Text` (`message.go:1440-1463`). Если `Messages.New` в фейке ругается на отсутствующий заголовок версии, SDK шлёт `anthropic-version` сам.

- [ ] **Step 3: Тесты** — `go test ./services/ -v` → PASS. Негативный контроль: сделать одну попытку вместо двух — `TestTranslateRetriesOnceOnMismatch` красный. Вернуть.

- [ ] **Step 4: Коммит**

```bash
git add services/anthropic.go services/anthropic_test.go go.mod go.sum
git commit -m "translator: upstream implementation with one retry on line mismatch"
```

---

### Task 5: `Store` — ход работы и финал (память + Redis/S3)

**Files:**
- Create: `services/store.go`, `services/redis_store.go`
- Test: `services/store_test.go`

**Interfaces:**
- Produces:
  ```go
  type Progress struct{ Total int; Lines []string } // Lines[i]=="" → не переведено
  type Store interface {
      GetFinal(ctx context.Context, key string) ([]byte, bool, error)       // S3 tr/<key>.vtt
      PutFinal(ctx context.Context, key string, vtt []byte) error
      GetProgress(ctx context.Context, key string) (*Progress, error)      // nil, nil если нет
      PutProgress(ctx context.Context, key string, p *Progress) error      // TTL 24h
      DropProgress(ctx context.Context, key string) error
      TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) // SETNX tr:lock:<key>
      RefreshLock(ctx context.Context, key string, ttl time.Duration) error
      Unlock(ctx context.Context, key string) error
  }
  func NewMemoryStore() *MemoryStore
  func RegisterStoreFlags(f []cli.Flag) []cli.Flag   // --use-s3 USE_S3, --aws-bucket AWS_BUCKET (default subtitle-translate), --s3-prefix (default "")
  func NewRedisStore(c *cli.Context, rc *cs.RedisClient, s3c *cs.S3Client) *RedisStore
  ```
- `RedisStore.GetFinal/PutFinal` идут в S3 только если `--use-s3` и клиент не nil; иначе финал хранится в Redis под `tr:final:<key>` без TTL (для self-hosted без S3).

- [ ] **Step 1: Падающий тест (контракт на памяти)**

```go
// services/store_test.go
package services

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStoreContract(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	if p, err := s.GetProgress(ctx, "k"); err != nil || p != nil {
		t.Fatalf("empty progress: %v %v", p, err)
	}
	if err := s.PutProgress(ctx, "k", &Progress{Total: 3, Lines: []string{"a", "", ""}}); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProgress(ctx, "k")
	if p == nil || p.Total != 3 || p.Lines[0] != "a" {
		t.Fatalf("progress=%+v", p)
	}
	ok, _ := s.TryLock(ctx, "k", time.Minute)
	ok2, _ := s.TryLock(ctx, "k", time.Minute)
	if !ok || ok2 {
		t.Fatalf("lock: first=%v second=%v", ok, ok2)
	}
	_ = s.Unlock(ctx, "k")
	if ok3, _ := s.TryLock(ctx, "k", time.Minute); !ok3 {
		t.Fatal("lock must be free after Unlock")
	}
	if _, found, _ := s.GetFinal(ctx, "k"); found {
		t.Fatal("no final yet")
	}
	_ = s.PutFinal(ctx, "k", []byte("WEBVTT\n"))
	if b, found, _ := s.GetFinal(ctx, "k"); !found || string(b) != "WEBVTT\n" {
		t.Fatalf("final=%q found=%v", b, found)
	}
	_ = s.DropProgress(ctx, "k")
	if p, _ := s.GetProgress(ctx, "k"); p != nil {
		t.Fatal("progress must be dropped")
	}
}
```

Run: `go test ./services/ -run TestMemoryStoreContract -v` → FAIL, undefined.

- [ ] **Step 2: Реализация**

```go
// services/store.go
package services

import (
	"context"
	"sync"
	"time"
)

type Progress struct {
	Total int
	Lines []string
}

type Store interface {
	GetFinal(ctx context.Context, key string) ([]byte, bool, error)
	PutFinal(ctx context.Context, key string, vtt []byte) error
	GetProgress(ctx context.Context, key string) (*Progress, error)
	PutProgress(ctx context.Context, key string, p *Progress) error
	DropProgress(ctx context.Context, key string) error
	TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	RefreshLock(ctx context.Context, key string, ttl time.Duration) error
	Unlock(ctx context.Context, key string) error
}

type MemoryStore struct {
	mu       sync.Mutex
	final    map[string][]byte
	progress map[string]*Progress
	locks    map[string]time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{final: map[string][]byte{}, progress: map[string]*Progress{}, locks: map[string]time.Time{}}
}

func (m *MemoryStore) GetFinal(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.final[key]
	return b, ok, nil
}

func (m *MemoryStore) PutFinal(_ context.Context, key string, vtt []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.final[key] = append([]byte(nil), vtt...)
	return nil
}

func (m *MemoryStore) GetProgress(_ context.Context, key string) (*Progress, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.progress[key]
	if !ok {
		return nil, nil
	}
	cp := &Progress{Total: p.Total, Lines: append([]string(nil), p.Lines...)}
	return cp, nil
}

func (m *MemoryStore) PutProgress(_ context.Context, key string, p *Progress) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.progress[key] = &Progress{Total: p.Total, Lines: append([]string(nil), p.Lines...)}
	return nil
}

func (m *MemoryStore) DropProgress(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.progress, key)
	return nil
}

func (m *MemoryStore) TryLock(_ context.Context, key string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if until, ok := m.locks[key]; ok && time.Now().Before(until) {
		return false, nil
	}
	m.locks[key] = time.Now().Add(ttl)
	return true, nil
}

func (m *MemoryStore) RefreshLock(_ context.Context, key string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.locks[key] = time.Now().Add(ttl)
	return nil
}

func (m *MemoryStore) Unlock(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.locks, key)
	return nil
}
```

```go
// services/redis_store.go
package services

import (
	"bytes"
	"context"
	"encoding/gob"
	"io"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
)

const (
	flagUseS3    = "use-s3"
	flagBucket   = "aws-bucket"
	flagS3Prefix = "s3-prefix"
	progressTTL  = 24 * time.Hour
)

func RegisterStoreFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolFlag{Name: flagUseS3, Usage: "store finished translations in S3", EnvVar: "USE_S3"},
		cli.StringFlag{Name: flagBucket, Usage: "S3 bucket (one bucket per service)", Value: "subtitle-translate", EnvVar: "AWS_BUCKET"},
		cli.StringFlag{Name: flagS3Prefix, Usage: "optional S3 key prefix", Value: "", EnvVar: "S3_PREFIX"},
	)
}

type RedisStore struct {
	rc     *cs.RedisClient
	s3c    *cs.S3Client
	useS3  bool
	bucket string
	prefix string
}

func NewRedisStore(c *cli.Context, rc *cs.RedisClient, s3c *cs.S3Client) *RedisStore {
	return &RedisStore{rc: rc, s3c: s3c, useS3: c.Bool(flagUseS3) && s3c != nil, bucket: c.String(flagBucket), prefix: c.String(flagS3Prefix)}
}

func (r *RedisStore) GetFinal(ctx context.Context, key string) ([]byte, bool, error) {
	if !r.useS3 {
		b, err := r.rc.Get().Get(ctx, "tr:final:"+key).Bytes()
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return b, err == nil, err
	}
	out, err := r.s3c.Get().GetObjectWithContext(ctx, &s3.GetObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(r.prefix + key + ".vtt")})
	if err != nil {
		if ae, ok := err.(awserr.Error); ok && ae.Code() == s3.ErrCodeNoSuchKey {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, "s3 get")
	}
	defer out.Body.Close()
	b, err := io.ReadAll(out.Body)
	return b, err == nil, err
}

func (r *RedisStore) PutFinal(ctx context.Context, key string, vtt []byte) error {
	if !r.useS3 {
		return r.rc.Get().Set(ctx, "tr:final:"+key, vtt, 0).Err()
	}
	_, err := r.s3c.Get().PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket: aws.String(r.bucket), Key: aws.String(r.prefix + key + ".vtt"),
		Body: bytes.NewReader(vtt), ContentType: aws.String("text/vtt; charset=utf-8"),
	})
	return errors.Wrap(err, "s3 put")
}

func (r *RedisStore) GetProgress(ctx context.Context, key string) (*Progress, error) {
	b, err := r.rc.Get().Get(ctx, "tr:cues:"+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "redis get progress")
	}
	var p Progress
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&p); err != nil {
		return nil, errors.Wrap(err, "decode progress")
	}
	return &p, nil
}

func (r *RedisStore) PutProgress(ctx context.Context, key string, p *Progress) error {
	buf := &bytes.Buffer{}
	if err := gob.NewEncoder(buf).Encode(p); err != nil {
		return errors.Wrap(err, "encode progress")
	}
	return r.rc.Get().Set(ctx, "tr:cues:"+key, buf.Bytes(), progressTTL).Err()
}

func (r *RedisStore) DropProgress(ctx context.Context, key string) error {
	return r.rc.Get().Del(ctx, "tr:cues:"+key).Err()
}

func (r *RedisStore) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return r.rc.Get().SetNX(ctx, "tr:lock:"+key, "1", ttl).Result()
}

func (r *RedisStore) RefreshLock(ctx context.Context, key string, ttl time.Duration) error {
	return r.rc.Get().Expire(ctx, "tr:lock:"+key, ttl).Err()
}

func (r *RedisStore) Unlock(ctx context.Context, key string) error {
	return r.rc.Get().Del(ctx, "tr:lock:"+key).Err()
}
```

`go get github.com/aws/aws-sdk-go github.com/redis/go-redis/v9` (версии подтянутся из `common-services`). `RedisStore` покрывается ручной проверкой в Task 9; юнит-контракт на `MemoryStore`.

- [ ] **Step 3: Тесты и сборка** — `go build ./... && go vet ./... && go test ./... -v` → PASS.

- [ ] **Step 4: Коммит**

```bash
git add services/store.go services/redis_store.go services/store_test.go go.mod go.sum
git commit -m "store: progress and final artifacts in memory (tests) and redis+s3 (prod)"
```

---

### Task 6: `Runner` — ключ, singleflight, фоновая задача, прогресс, возобновление

**Files:**
- Create: `services/job.go`
- Test: `services/job_test.go`

**Interfaces:**
- Consumes: `Store`, `Translator`, `Doc`, `Batches`, `JoinLines`, `LangName`, метрики.
- Produces:
  ```go
  func ArtifactKey(infoHash, path, lang, model, promptVersion string) string // hex sha256
  type Job struct{ Lang, SourceLang string; Glossary []string; Doc *Doc }
  type Snapshot struct{ Body []byte; Done, Total int; Final bool }
  type Runner struct{ ... }
  func NewRunner(store Store, tr Translator, model string, batchSize int, lockTTL time.Duration) *Runner
  func (r *Runner) Snapshot(ctx context.Context, key string, doc *Doc) (*Snapshot, error)  // без запуска
  func (r *Runner) Ensure(ctx context.Context, key string, job *Job)                        // запускает фон, если не запущен и нет финала
  func (r *Runner) Wait(key string)                                                          // для тестов
  ```
- Поведение `run(key, job)`: `TryLock` (нет → выйти; кто-то работает); прогресс из store или новый `Progress{Total: len(cues)}`; для батчей по порядку, пропуская уже переведённые (все `Lines[from:to]` непустые), формируется `BatchRequest` (пустые cue после нормализации не отправляются: их индексы пропускаются и остаются `""`; контекст = последние 5 непустых переведённых); результат пишется в `Progress`, `PutProgress`, `RefreshLock`, `BatchesTotal.Inc()`; при `ErrLineMismatch` батч заполняется оригинальными текстами; при другой ошибке `JobErrors{code="upstream"}`, `Unlock`, выход (прогресс сохранён). По завершении `Render(lines, total)` → `PutFinal` → `DropProgress` → `Unlock`, `JobDuration`.

- [ ] **Step 1: Падающие тесты**

```go
// services/job_test.go
package services

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTranslator struct {
	calls int32
	delay time.Duration
	fail  error
	block chan struct{}
}

func (f *fakeTranslator) Translate(_ context.Context, req BatchRequest) (BatchResult, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.block != nil {
		<-f.block
	}
	time.Sleep(f.delay)
	if f.fail != nil {
		return BatchResult{}, f.fail
	}
	out := make([]string, len(req.Lines))
	for i, l := range req.Lines {
		out[i] = "PT:" + l
	}
	return BatchResult{Lines: out, InputTokens: 1, OutputTokens: 1}, nil
}

// vttWith builds n cues with increasing timings: "line 1" … "line n".
func vttWith(n int) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%d\n00:00:%02d.000 --> 00:00:%02d.500\nline %d\n\n", i+1, i, i, i+1)
	}
	return b.String()
}

func TestArtifactKeyStable(t *testing.T) {
	a := ArtifactKey("h", "/a.srt~vtt/a.vtt", "pt", "m", "v1")
	b := ArtifactKey("h", "/a.srt~vtt/a.vtt", "pt", "m", "v1")
	c := ArtifactKey("h", "/a.srt~vtt/a.vtt", "es", "m", "v1")
	if a != b || a == c || len(a) != 64 {
		t.Fatalf("a=%s b=%s c=%s", a, b, c)
	}
}

func TestRunnerProgressiveThenFinal(t *testing.T) {
	doc, _ := ParseVTT(strings.NewReader(vttWith(7)))
	doc.Normalize()
	ft := &fakeTranslator{block: make(chan struct{})}
	r := NewRunner(NewMemoryStore(), ft, "m", 3, time.Minute)
	key := "k1"
	snap, _ := r.Snapshot(context.Background(), key, doc)
	if snap.Done != 0 || snap.Final || !strings.HasPrefix(string(snap.Body), "WEBVTT") {
		t.Fatalf("initial snapshot=%+v", snap)
	}
	r.Ensure(context.Background(), key, &Job{Lang: "pt", Doc: doc})
	r.Ensure(context.Background(), key, &Job{Lang: "pt", Doc: doc}) // second call must not start a second job
	ft.block <- struct{}{}                                         // release batch 1 only
	time.Sleep(50 * time.Millisecond)
	snap, _ = r.Snapshot(context.Background(), key, doc)
	if snap.Done != 3 || snap.Final || !strings.Contains(string(snap.Body), "PT:line 3") || strings.Contains(string(snap.Body), "line 4") {
		t.Fatalf("after batch 1: done=%d final=%v body=%q", snap.Done, snap.Final, snap.Body)
	}
	close(ft.block)
	r.Wait(key)
	snap, _ = r.Snapshot(context.Background(), key, doc)
	if !snap.Final || snap.Done != 100 || snap.Total != 100 || !strings.Contains(string(snap.Body), "PT:line 7") {
		t.Fatalf("final: %+v", snap)
	}
	if atomic.LoadInt32(&ft.calls) != 3 {
		t.Fatalf("calls=%d want 3 batches", ft.calls)
	}
}

func TestRunnerResumesFromStoredProgress(t *testing.T) {
	doc, _ := ParseVTT(strings.NewReader(vttWith(6)))
	doc.Normalize()
	st := NewMemoryStore()
	_ = st.PutProgress(context.Background(), "k2", &Progress{Total: 6, Lines: []string{"PT:line 1", "PT:line 2", "PT:line 3", "", "", ""}})
	ft := &fakeTranslator{}
	r := NewRunner(st, ft, "m", 3, time.Minute)
	r.Ensure(context.Background(), "k2", &Job{Lang: "pt", Doc: doc})
	r.Wait("k2")
	if atomic.LoadInt32(&ft.calls) != 1 {
		t.Fatalf("calls=%d: must translate only the missing batch", ft.calls)
	}
	snap, _ := r.Snapshot(context.Background(), "k2", doc)
	if !snap.Final || !strings.Contains(string(snap.Body), "PT:line 1") || !strings.Contains(string(snap.Body), "PT:line 6") {
		t.Fatalf("final=%+v", snap)
	}
}

func TestRunnerKeepsOriginalOnLineMismatch(t *testing.T) {
	doc, _ := ParseVTT(strings.NewReader(vttWith(2)))
	doc.Normalize()
	ft := &fakeTranslator{fail: ErrLineMismatch}
	r := NewRunner(NewMemoryStore(), ft, "m", 50, time.Minute)
	r.Ensure(context.Background(), "k3", &Job{Lang: "pt", Doc: doc})
	r.Wait("k3")
	snap, _ := r.Snapshot(context.Background(), "k3", doc)
	if !snap.Final || !strings.Contains(string(snap.Body), "line 1") {
		t.Fatalf("mismatch must fall back to originals and still finish: %+v", snap)
	}
}

func TestRunnerStopsOnUpstreamErrorKeepingProgress(t *testing.T) {
	doc, _ := ParseVTT(strings.NewReader(vttWith(2)))
	doc.Normalize()
	ft := &fakeTranslator{fail: errors.New("boom")}
	st := NewMemoryStore()
	r := NewRunner(st, ft, "m", 50, time.Minute)
	r.Ensure(context.Background(), "k4", &Job{Lang: "pt", Doc: doc})
	r.Wait("k4")
	snap, _ := r.Snapshot(context.Background(), "k4", doc)
	if snap.Final || snap.Done != 0 {
		t.Fatalf("must not finish on upstream error: %+v", snap)
	}
	if ok, _ := st.TryLock(context.Background(), "k4", time.Minute); !ok {
		t.Fatal("lock must be released after a failed run")
	}
}

```

Импорт `fmt` в тесте. Ограничение фикстуры: `n <= 60`.

Run: `go test ./services/ -run 'TestArtifactKey|TestRunner' -v` → FAIL, undefined.

- [ ] **Step 2: Реализация**

```go
// services/job.go
package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func ArtifactKey(infoHash, path, lang, model, promptVersion string) string {
	sum := sha256.Sum256([]byte(infoHash + "\x00" + path + "\x00" + lang + "\x00" + model + "\x00" + promptVersion))
	return hex.EncodeToString(sum[:])
}

type Job struct {
	Lang       string
	SourceLang string
	Glossary   []string
	Doc        *Doc
}

type Snapshot struct {
	Body  []byte
	Done  int
	Total int
	Final bool
}

type Runner struct {
	store     Store
	tr        Translator
	model     string
	batchSize int
	lockTTL   time.Duration
	mu        sync.Mutex
	running   map[string]chan struct{}
}

func NewRunner(store Store, tr Translator, model string, batchSize int, lockTTL time.Duration) *Runner {
	return &Runner{store: store, tr: tr, model: model, batchSize: batchSize, lockTTL: lockTTL, running: map[string]chan struct{}{}}
}

// Snapshot renders what is known for key without starting anything.
func (r *Runner) Snapshot(ctx context.Context, key string, doc *Doc) (*Snapshot, error) {
	// A finished artifact is served without re-reading the source, so the
	// cue count is unknown here: progress is reported as 100/100 (the
	// player treats done == total as complete).
	if b, ok, err := r.store.GetFinal(ctx, key); err != nil {
		return nil, err
	} else if ok {
		return &Snapshot{Body: b, Done: 100, Total: 100, Final: true}, nil
	}
	p, err := r.store.GetProgress(ctx, key)
	if err != nil {
		return nil, err
	}
	total := len(doc.Cues)
	var lines []string
	if p != nil {
		lines = p.Lines
	}
	done := countDone(lines, doc)
	body, err := doc.Render(lines, done)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Body: body, Done: done, Total: total}, nil
}

// countDone is the length of the translated prefix: a cue counts as done
// when it has a translation or was empty after normalization.
func countDone(lines []string, doc *Doc) int {
	n := 0
	for i := range doc.Cues {
		if len(doc.Cues[i].Lines) == 0 || (i < len(lines) && lines[i] != "") {
			n++
			continue
		}
		break
	}
	return n
}

// Ensure starts the background job once per key per process.
func (r *Runner) Ensure(ctx context.Context, key string, job *Job) {
	r.mu.Lock()
	if _, ok := r.running[key]; ok {
		r.mu.Unlock()
		return
	}
	done := make(chan struct{})
	r.running[key] = done
	r.mu.Unlock()
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.running, key)
			r.mu.Unlock()
			close(done)
		}()
		r.run(context.Background(), key, job)
	}()
}

func (r *Runner) Wait(key string) {
	r.mu.Lock()
	ch, ok := r.running[key]
	r.mu.Unlock()
	if ok {
		<-ch
	}
}

func (r *Runner) run(ctx context.Context, key string, job *Job) {
	start := time.Now()
	logger := log.WithFields(log.Fields{"key": key[:12], "lang": job.Lang, "cues": len(job.Doc.Cues)})
	if _, ok, err := r.store.GetFinal(ctx, key); err == nil && ok {
		return
	}
	locked, err := r.store.TryLock(ctx, key, r.lockTTL)
	if err != nil || !locked {
		return
	}
	defer func() { _ = r.store.Unlock(ctx, key) }()

	p, err := r.store.GetProgress(ctx, key)
	if err != nil {
		JobErrors.WithLabelValues("store").Inc()
		logger.WithError(err).Error("failed to load progress")
		return
	}
	if p == nil || len(p.Lines) != len(job.Doc.Cues) {
		p = &Progress{Total: len(job.Doc.Cues), Lines: make([]string, len(job.Doc.Cues))}
	}
	targetName, _ := LangName(job.Lang)
	for _, b := range Batches(len(job.Doc.Cues), r.batchSize) {
		idx, lines := pendingInBatch(job.Doc, p.Lines, b[0], b[1])
		if len(idx) == 0 {
			continue
		}
		req := BatchRequest{TargetLang: job.Lang, TargetName: targetName, SourceLang: job.SourceLang, Glossary: job.Glossary, Context: lastTranslated(p.Lines, b[0], 5), Lines: lines}
		res, err := r.tr.Translate(ctx, req)
		switch {
		case err == nil:
			for i, li := range idx {
				p.Lines[li] = res.Lines[i]
			}
		case errors.Is(err, ErrLineMismatch):
			logger.WithField("batch", b).Warn("line mismatch twice, keeping originals")
			for i, li := range idx {
				p.Lines[li] = lines[i]
			}
		default:
			JobErrors.WithLabelValues("upstream").Inc()
			logger.WithError(err).WithField("batch", b).Error("upstream failed, stopping")
			_ = r.store.PutProgress(ctx, key, p)
			return
		}
		BatchesTotal.Inc()
		if err := r.store.PutProgress(ctx, key, p); err != nil {
			JobErrors.WithLabelValues("store").Inc()
			logger.WithError(err).Error("failed to store progress")
			return
		}
		_ = r.store.RefreshLock(ctx, key, r.lockTTL)
	}
	body, err := job.Doc.Render(p.Lines, len(job.Doc.Cues))
	if err != nil {
		JobErrors.WithLabelValues("render").Inc()
		logger.WithError(err).Error("failed to render final")
		return
	}
	if err := r.store.PutFinal(ctx, key, body); err != nil {
		JobErrors.WithLabelValues("store").Inc()
		logger.WithError(err).Error("failed to store final")
		return
	}
	_ = r.store.DropProgress(ctx, key)
	JobDuration.Observe(time.Since(start).Seconds())
	logger.WithField("seconds", time.Since(start).Seconds()).Info("translation finished")
}

// pendingInBatch returns cue indexes in [from,to) that still need a
// translation (non-empty after normalization and not yet translated).
func pendingInBatch(doc *Doc, lines []string, from, to int) ([]int, []string) {
	var idx []int
	var texts []string
	for i := from; i < to; i++ {
		if len(doc.Cues[i].Lines) == 0 || lines[i] != "" {
			continue
		}
		idx = append(idx, i)
		texts = append(texts, JoinLines(doc.Cues[i]))
	}
	return idx, texts
}

func lastTranslated(lines []string, before, n int) []string {
	var out []string
	for i := before - 1; i >= 0 && len(out) < n; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			out = append([]string{lines[i]}, out...)
		}
	}
	return out
}
```

Замечание: в `TestRunnerProgressiveThenFinal` после первого батча `Done` должно быть 3; `countDone` считает переведённый префикс, и cue, пустые после нормализации, считаются готовыми.

- [ ] **Step 3: Тесты** — `go test ./services/ -race -v` → PASS.

- [ ] **Step 4: Коммит**

```bash
git add services/job.go services/job_test.go
git commit -m "runner: per-key background translation with progress, resume and lock"
```

---

### Task 7: HTTP-обработчик и сборка сервиса

**Files:**
- Modify: `services/web.go` (добавить `Handler`), `configure.go`
- Test: `services/handler_test.go`

**Interfaces:**
- Consumes: `Runner`, `Store`, `Translator`, `ParseVTT`, `ArtifactKey`, `LangName`, `PromptVersion`.
- Produces: `type Handler struct{ Runner *Runner; Model string; Client *http.Client; MaxSourceBytes int64; MaxCues int }`; `func (h *Handler) ServeHTTP(w, r)`; `func ParseLang(path string) (string, bool)` (regexp `~tr:([a-z]{2})/[^/]*\.vtt$` по декодированному пути); `func ParseNames(q string) []string` (до 30, обрезка по 40 символов).
- Поведение: `HEAD` и `GET`: 400 без языка или языка вне списка; 400 без `X-Source-Url`; ключ по `X-Info-Hash`, `X-Path`, lang, model, `PromptVersion`; финал в store → 200 полный, `X-Subtitle-Progress: N/N`, `Cache-Control: public, max-age=86400`. Иначе: `HEAD` → прогресс из store без запуска (если прогресса нет: `0/0`), 200 без тела. `GET` → выборка источника (`GET X-Source-Url` с `io.LimitReader(MaxSourceBytes+1)`, 30 с таймаут), 404 при ошибке, 413 при превышении, `ParseVTT`, `Normalize`, 413 при `len(Cues) > MaxCues`, `Runner.Ensure`, `Runner.Snapshot` → 200 частичный, `no-store`. Флаги: `--batch-size` (50), `--max-cues` (5000), `--max-source-bytes` (1048576), `--lock-ttl` (600 с).

- [ ] **Step 1: Падающие тесты**

```go
// services/handler_test.go
package services

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newHandlerForTest(t *testing.T, tr Translator, source string) (*Handler, *httptest.Server) {
	t.Helper()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if source == "" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(source))
	}))
	t.Cleanup(src.Close)
	h := &Handler{Runner: NewRunner(NewMemoryStore(), tr, "m", 3, time.Minute), Model: "m", Client: src.Client(), MaxSourceBytes: 1 << 20, MaxCues: 5000}
	return h, src
}

func do(h http.Handler, method, path, sourceURL string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Info-Hash", "abc")
	req.Header.Set("X-Path", "/movie.srt~vtt/movie.vtt")
	if sourceURL != "" {
		req.Header.Set("X-Source-Url", sourceURL)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestParseLang(t *testing.T) {
	for p, want := range map[string]string{
		"/abc/movie.srt~vtt/movie.vtt~tr:pt/movie.vtt": "pt",
		"/abc/movie.mkv~vi/opensubtitles/1.vtt~tr:ru/1.vtt": "ru",
	} {
		if got, ok := ParseLang(p); !ok || got != want {
			t.Errorf("%s: got %q ok=%v", p, got, ok)
		}
	}
	for _, p := range []string{"/abc/movie.vtt", "/abc/movie.vtt~tr:xx/movie.vtt", "/abc/movie.vtt~tr:pt/movie.srt", "/abc/movie.vtt~tr:PT/movie.vtt"} {
		if _, ok := ParseLang(p); ok {
			t.Errorf("%s must be rejected", p)
		}
	}
}

func TestHandlerRejectsBadRequests(t *testing.T) {
	h, src := newHandlerForTest(t, &fakeTranslator{}, vttWith(2))
	if rec := do(h, "GET", "/abc/movie.vtt", src.URL); rec.Code != 400 {
		t.Fatalf("no lang: %d", rec.Code)
	}
	if rec := do(h, "GET", "/abc/movie.vtt~tr:pt/movie.vtt", ""); rec.Code != 400 {
		t.Fatalf("no source: %d", rec.Code)
	}
	h2, src2 := newHandlerForTest(t, &fakeTranslator{}, "")
	if rec := do(h2, "GET", "/abc/movie.vtt~tr:pt/movie.vtt", src2.URL); rec.Code != 404 {
		t.Fatalf("source 404 must map to 404: %d", rec.Code)
	}
	h.MaxCues = 1
	if rec := do(h, "GET", "/abc/movie.vtt~tr:pt/movie.vtt", src.URL); rec.Code != 413 {
		t.Fatalf("too many cues: %d", rec.Code)
	}
}

func TestHandlerProgressiveGetAndHead(t *testing.T) {
	ft := &fakeTranslator{block: make(chan struct{})}
	h, src := newHandlerForTest(t, ft, vttWith(5))
	path := "/abc/movie.srt~vtt/movie.vtt~tr:pt/movie.vtt"
	rec := do(h, "GET", path, src.URL)
	if rec.Code != 200 || rec.Header().Get("X-Subtitle-Progress") != "0/5" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Content-Type") != "text/vtt; charset=utf-8" {
		t.Fatalf("first get: code=%d headers=%v", rec.Code, rec.Header())
	}
	if !strings.HasPrefix(rec.Body.String(), "WEBVTT") {
		t.Fatalf("body=%q", rec.Body.String())
	}
	ft.block <- struct{}{}
	time.Sleep(50 * time.Millisecond)
	rec = do(h, "HEAD", path, src.URL)
	if rec.Code != 200 || rec.Header().Get("X-Subtitle-Progress") != "3/5" || rec.Body.Len() != 0 {
		t.Fatalf("head: code=%d progress=%s body=%d", rec.Code, rec.Header().Get("X-Subtitle-Progress"), rec.Body.Len())
	}
	close(ft.block)
	key := ArtifactKey("abc", "/movie.srt~vtt/movie.vtt", "pt", "m", PromptVersion)
	h.Runner.Wait(key)
	rec = do(h, "GET", path, src.URL)
	if rec.Code != 200 || rec.Header().Get("X-Subtitle-Progress") != "100/100" || rec.Header().Get("Cache-Control") != "public, max-age=86400" || !strings.Contains(rec.Body.String(), "PT:line 5") {
		t.Fatalf("final get: code=%d headers=%v body=%q", rec.Code, rec.Header(), rec.Body.String())
	}
}

func TestHeadDoesNotStartJob(t *testing.T) {
	ft := &fakeTranslator{}
	h, src := newHandlerForTest(t, ft, vttWith(2))
	rec := do(h, "HEAD", "/abc/movie.vtt~tr:pt/movie.vtt", src.URL)
	if rec.Code != 200 || rec.Header().Get("X-Subtitle-Progress") != "0/0" {
		t.Fatalf("head before any get: %d %s", rec.Code, rec.Header().Get("X-Subtitle-Progress"))
	}
	time.Sleep(30 * time.Millisecond)
	if ft.calls != 0 {
		t.Fatal("HEAD must not translate")
	}
}
```

Run: `go test ./services/ -run 'TestParseLang|TestHandler|TestHead' -v` → FAIL, undefined.

- [ ] **Step 2: Реализация**

Добавить в `services/web.go`:

```go
var trPathRe = regexp.MustCompile(`~tr:([a-z]{2})/[^/]*\.vtt$`)

// ParseLang takes the target language from the request path: THP does
// not forward the mod extra, but the reverse proxy keeps the original
// path, so /…~tr:pt/name.vtt is visible here.
func ParseLang(p string) (string, bool) {
	m := trPathRe.FindStringSubmatch(p)
	if m == nil {
		return "", false
	}
	if _, ok := LangName(m[1]); !ok {
		return "", false
	}
	return m[1], true
}

func ParseNames(q string) []string {
	var out []string
	for _, n := range strings.Split(q, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if len(n) > 40 {
			n = n[:40]
		}
		out = append(out, n)
		if len(out) == 30 {
			break
		}
	}
	return out
}

type Handler struct {
	Runner         *Runner
	Model          string
	Client         *http.Client
	MaxSourceBytes int64
	MaxCues        int
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lang, ok := ParseLang(r.URL.Path)
	if !ok {
		http.Error(w, "unsupported or missing target language", http.StatusBadRequest)
		return
	}
	sourceURL := r.Header.Get("X-Source-Url")
	if sourceURL == "" {
		http.Error(w, "missing X-Source-Url", http.StatusBadRequest)
		return
	}
	key := ArtifactKey(r.Header.Get("X-Info-Hash"), r.Header.Get("X-Path"), lang, h.Model, PromptVersion)
	logger := log.WithFields(log.Fields{"key": key[:12], "lang": lang, "infoHash": r.Header.Get("X-Info-Hash"), "path": r.Header.Get("X-Path")})
	ctx := r.Context()

	if body, ok, err := h.Runner.store.GetFinal(ctx, key); err == nil && ok {
		writeVTT(w, r, body, 100, 100, true)
		return
	}
	if r.Method == http.MethodHead {
		p, _ := h.Runner.store.GetProgress(ctx, key)
		done, total := 0, 0
		if p != nil {
			total = p.Total
			for _, l := range p.Lines {
				if l == "" {
					break
				}
				done++
			}
		}
		writeVTT(w, r, nil, done, total, false)
		return
	}
	doc, status, err := h.fetchDoc(ctx, sourceURL)
	if err != nil {
		logger.WithError(err).Warn("source unavailable")
		http.Error(w, err.Error(), status)
		return
	}
	h.Runner.Ensure(ctx, key, &Job{Lang: lang, SourceLang: r.URL.Query().Get("srclang"), Glossary: ParseNames(r.URL.Query().Get("names")), Doc: doc})
	snap, err := h.Runner.Snapshot(ctx, key, doc)
	if err != nil {
		logger.WithError(err).Error("snapshot failed")
		http.Error(w, "upstream state unavailable", http.StatusBadGateway)
		return
	}
	writeVTT(w, r, snap.Body, snap.Done, snap.Total, snap.Final)
}

func (h *Handler) fetchDoc(ctx context.Context, sourceURL string) (*Doc, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, http.StatusBadRequest, errors.Wrap(err, "bad source url")
	}
	res, err := h.Client.Do(req)
	if err != nil {
		return nil, http.StatusNotFound, errors.Wrap(err, "source fetch failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, http.StatusNotFound, errors.Errorf("source returned %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, h.MaxSourceBytes+1))
	if err != nil {
		return nil, http.StatusNotFound, errors.Wrap(err, "source read failed")
	}
	if int64(len(data)) > h.MaxSourceBytes {
		return nil, http.StatusRequestEntityTooLarge, errors.New("source too large")
	}
	doc, err := ParseVTT(bytes.NewReader(data))
	if err != nil {
		return nil, http.StatusNotFound, err
	}
	if len(doc.Cues) > h.MaxCues {
		return nil, http.StatusRequestEntityTooLarge, errors.Errorf("too many cues: %d", len(doc.Cues))
	}
	doc.Normalize()
	return doc, 0, nil
}

func writeVTT(w http.ResponseWriter, r *http.Request, body []byte, done, total int, final bool) {
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("X-Subtitle-Progress", strconv.Itoa(done)+"/"+strconv.Itoa(total))
	if final {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead || body == nil {
		return
	}
	_, _ = w.Write(body)
}
```

Импорты `web.go` после этой задачи: `bytes`, `context`, `fmt`, `io`, `net`, `net/http`, `regexp`, `strconv`, `strings`, `time`, logrus-middleware, `pkg/errors`, logrus, `urfave/cli`. Финал отдаётся как `100/100` (см. `Runner.Snapshot`).

Флаги и сборка в `configure.go`:

```go
const (
	flagBatchSize      = "batch-size"
	flagMaxCues        = "max-cues"
	flagMaxSourceBytes = "max-source-bytes"
	flagLockTTL        = "lock-ttl"
)

func configure(app *cli.App) {
	app.Flags = []cli.Flag{}
	app.Flags = cs.RegisterProbeFlags(app.Flags)
	app.Flags = cs.RegisterPromFlags(app.Flags)
	app.Flags = services.RegisterWebFlags(app.Flags)
	app.Flags = services.RegisterTranslatorFlags(app.Flags)
	app.Flags = cs.RegisterRedisClientFlags(app.Flags)
	app.Flags = cs.RegisterS3ClientFlags(app.Flags)
	app.Flags = services.RegisterStoreFlags(app.Flags)
	app.Flags = append(app.Flags,
		cli.IntFlag{Name: flagBatchSize, Value: 50, EnvVar: "SUBTITLE_TRANSLATE_BATCH_SIZE"},
		cli.IntFlag{Name: flagMaxCues, Value: 5000, EnvVar: "SUBTITLE_TRANSLATE_MAX_CUES"},
		cli.Int64Flag{Name: flagMaxSourceBytes, Value: 1 << 20, EnvVar: "SUBTITLE_TRANSLATE_MAX_SOURCE_BYTES"},
		cli.IntFlag{Name: flagLockTTL, Value: 600, EnvVar: "SUBTITLE_TRANSLATE_LOCK_TTL"},
	)
	app.Action = run
}

func run(c *cli.Context) error {
	var servers []cs.Servable
	if probe := cs.NewProbe(c); probe != nil {
		servers = append(servers, probe)
		defer probe.Close()
	}
	if prom := cs.NewProm(c); prom != nil {
		servers = append(servers, prom)
		defer prom.Close()
	}
	var handler http.Handler = services.NotConfiguredHandler()
	if tr := services.NewAnthropicTranslator(c); tr != nil {
		rc := cs.NewRedisClient(c)
		defer rc.Close()
		s3c := cs.NewS3Client(c, &http.Client{Timeout: 60 * time.Second})
		store := services.NewRedisStore(c, rc, s3c)
		model := tr.(*services.AnthropicTranslator).Model()
		runner := services.NewRunner(store, tr, model, c.Int(flagBatchSize), time.Duration(c.Int(flagLockTTL))*time.Second)
		handler = &services.Handler{Runner: runner, Model: model, Client: &http.Client{Timeout: 35 * time.Second}, MaxSourceBytes: c.Int64(flagMaxSourceBytes), MaxCues: c.Int(flagMaxCues)}
	}
	web := services.NewWeb(c, handler)
	servers = append(servers, web)
	defer web.Close()
	if err := cs.NewServe(servers...).Serve(); err != nil {
		log.WithError(err).Error("got serve error")
		return err
	}
	return nil
}
```

`Handler` обращается к `h.Runner.store`, поле в том же пакете, доступ есть.

- [ ] **Step 3: Тесты и сборка** — `go build ./... && go vet ./... && go test ./... -race -v` → PASS.

- [ ] **Step 4: Коммит**

```bash
git add services/web.go services/handler_test.go services/job.go services/job_test.go configure.go
git commit -m "http: ~tr:<lang> handler with progressive GET, HEAD progress and limits"
```

---

### Task 8: README, репозиторий GitHub, первый образ

**Files:**
- Modify: `README.md`

- [ ] **Step 1: README**

Разделы: назначение; как вызывается через THP (`/…~tr:<lang>/<name>.vtt`), заголовки, что означает `X-Subtitle-Progress`; поддерживаемые коды языков (`services/langs.go`); флаги (вставить вывод `go run . --help`); локальный запуск:

```bash
ANTHROPIC_API_KEY=… REDIS_SERVICE_HOST=localhost go run . --use-probe=false --use-prom=false
curl -H 'X-Source-Url: http://localhost:8000/sample.vtt' -H 'X-Info-Hash: t' -H 'X-Path: /sample.vtt' \
  -D - 'http://localhost:8080/t/sample.vtt~tr:pt/sample.vtt'
```

Оговорка про гейт: сервис не проверяет права, доступ ограничивает web-ui.

- [ ] **Step 2: Репозиторий и первый пуш**

```bash
cd /Users/vintikzzzz/Projects/webtor/subtitle-translate && git add README.md && git commit -m "docs: README"
gh repo create webtor-io/subtitle-translate --public --source=. --remote=origin --push --description "Translates WebVTT subtitles into the viewer's language (webtor ~tr mod)"
gh run list -R webtor-io/subtitle-translate -L 1
```

Дождаться `completed success`. Проверить публичность пакета GHCR: `curl -sI https://ghcr.io/v2/webtor-io/subtitle-translate/manifests/sha-<7> -H "Authorization: Bearer $(curl -s 'https://ghcr.io/token?scope=repository:webtor-io/subtitle-translate:pull' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')"` → 200. Если 401, в GitHub Packages сделать пакет public (владелец).

---

### Task 9: Чарт, values, релиз, роутинг THP, деплой, смоук

**Files (infra/helmfile):**
- Create: `charts/subtitle-translate/Chart.yaml`, `values.yaml`, `templates/_helpers.tpl`, `templates/deployment.yaml`, `templates/service.yaml`
- Create: `values/subtitle-translate.yaml.gotmpl`
- Modify: `helmfile.yaml` (после блока `srt2vtt`), `values/torrent-http-proxy/services.yaml`, `environments/default/images.yaml`

- [ ] **Step 1: Чарт**

Скопировать `charts/srt2vtt` в `charts/subtitle-translate`, заменить имя во всех шаблонах (`srt2vtt.` → `subtitle-translate.`, `name: srt2vtt` → `name: subtitle-translate`, `description`), в `values.yaml` `repository: ghcr.io/webtor-io/subtitle-translate`, добавить `securityContext: {}`, `imagePullSecrets: []`, и блок:

```yaml
redis:
  host: ""
  port: 6379
  pass: ""
aws:
  enabled: "false"
  accessKeyId: ""
  secretAccessKey: ""
  endpoint: ""
  region: ""
  bucket: "subtitle-translate"
  prefix: ""
  noSSL: "false"
translate:
  apiKey: ""
  model: "claude-haiku-4-5-20251001"
  batchSize: 50
  maxCues: 5000
```

В `templates/deployment.yaml` после `imagePullPolicy` вставить (по образцу `charts/video-info/templates/deployment.yaml:27-61`):

```yaml
          env:
            {{ if not (eq .Values.redis.host "") }}
            - name: REDIS_MASTER_SERVICE_HOST
              value: "{{ .Values.redis.host }}"
            - name: REDIS_MASTER_SERVICE_PORT
              value: "{{ .Values.redis.port }}"
            - name: REDIS_PASS
              value: "{{ .Values.redis.pass }}"
            {{ end }}
            - name: AWS_ACCESS_KEY_ID
              value: "{{ .Values.aws.accessKeyId }}"
            - name: AWS_SECRET_ACCESS_KEY
              value: "{{ .Values.aws.secretAccessKey }}"
            - name: AWS_BUCKET
              value: "{{ .Values.aws.bucket }}"
            - name: S3_PREFIX
              value: "{{ .Values.aws.prefix }}"
            - name: AWS_ENDPOINT
              value: "{{ .Values.aws.endpoint }}"
            - name: AWS_REGION
              value: "{{ .Values.aws.region }}"
            - name: AWS_NO_SSL
              value: "{{ .Values.aws.noSSL }}"
            - name: USE_S3
              value: "{{ .Values.aws.enabled }}"
            - name: ANTHROPIC_API_KEY
              value: "{{ .Values.translate.apiKey }}"
            - name: SUBTITLE_TRANSLATE_MODEL
              value: "{{ .Values.translate.model }}"
            - name: SUBTITLE_TRANSLATE_BATCH_SIZE
              value: "{{ .Values.translate.batchSize }}"
            - name: SUBTITLE_TRANSLATE_MAX_CUES
              value: "{{ .Values.translate.maxCues }}"
```

и порт `httpprom` 8083 в `ports:`. `helm lint charts/subtitle-translate` → чисто.

- [ ] **Step 2: values, релиз, роутинг, образ**

`values/subtitle-translate.yaml.gotmpl`:

```yaml
image:
  repository: "{{ .Values.images.prefix }}{{ .Release.Name }}"
  tag: {{ index .Values.images .Release.Name | default "latest" }}
replicaCount: 2
affinity: {{ .Values.affinity.workerPool | toJson }}
resources:
  requests:
    cpu: 50m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
redis:
  host: dragonfly
aws:
  enabled: "true"
  accessKeyId: "<из values/video-info.yaml.gotmpl aws.accessKeyId>"
  secretAccessKey: "<из values/video-info.yaml.gotmpl aws.secretAccessKey>"
  endpoint: "https://s3.de.io.cloud.ovh.net"
  region: "de"
  bucket: "subtitle-translate"
  prefix: ""
translate:
  apiKey: "<из values/web-ui.yaml.gotmpl anthropic.apiKey>"
  model: "claude-haiku-4-5-20251001"
```

`helmfile.yaml` после `srt2vtt`:
```yaml
- name: subtitle-translate
  namespace: webtor
  chart: charts/subtitle-translate
  values:
    - values/subtitle-translate.yaml.gotmpl
```

`values/torrent-http-proxy/services.yaml` в конец:
```yaml
tr:
  name: subtitle-translate
```

`environments/default/images.yaml`: строка `  subtitle-translate: sha-<7 из gh run>` с двумя пробелами.

- [ ] **Step 3: Бакет** (один бакет на сервис, решение владельца)

```bash
V=/Users/vintikzzzz/Projects/webtor/infra/helmfile/values/video-info.yaml.gotmpl
AWS_ACCESS_KEY_ID=$(awk '$1=="accessKeyId:"{gsub(/"/,"",$2);print $2}' $V) AWS_SECRET_ACCESS_KEY=$(awk '$1=="secretAccessKey:"{gsub(/"/,"",$2);print $2}' $V) \
  aws --endpoint-url https://s3.de.io.cloud.ovh.net --region de s3 mb s3://subtitle-translate
aws --endpoint-url https://s3.de.io.cloud.ovh.net --region de s3 ls | grep subtitle-translate
```
Если ключи video-info не имеют права создавать бакеты, бакет создаёт владелец в панели OVH (регион `de`, приватный), а в values подставляются ключи с доступом к нему.

- [ ] **Step 4: Деплой сервиса, затем THP**

```bash
cd /Users/vintikzzzz/Projects/webtor/infra/helmfile && helmfile --selector name=subtitle-translate diff | head -40
./sync.sh --wait subtitle-translate
kubectl rollout status deploy/subtitle-translate -n webtor --timeout=300s
kubectl logs -n webtor deploy/subtitle-translate --tail=5    # ожидается "serving web at :8080", без "translation disabled"
./sync.sh --force thp                                          # ConfigMap services.yaml → DaemonSet rollout
kubectl get cm -n webtor torrent-http-proxy -o jsonpath='{.data.services\.yaml}' | grep -A1 '^tr:'
```

- [ ] **Step 5: Смоук через THP**

Взять живой подписанный URL приложенного srt из логов web-ui или Network-панели (`…/name.srt~vtt/name.vtt?token=…`), добавить `~tr:pt/name.vtt` перед `?`:

```bash
U='<https://…/name.srt~vtt/name.vtt>'; Q='?<token=…&api-key=…>'
curl -s -D - -o /tmp/tr1.vtt "${U}~tr:pt/name.vtt${Q}" | grep -iE 'HTTP/|x-subtitle-progress|cache-control|content-type'
sleep 20; curl -s -I "${U}~tr:pt/name.vtt${Q}" | grep -i x-subtitle-progress
sleep 40; curl -s "${U}~tr:pt/name.vtt${Q}" | head -20
kubectl logs -n webtor deploy/subtitle-translate --since=5m | grep -E 'translation finished|upstream|mismatch' | tail -5
curl -s http://$(kubectl get svc -n webtor subtitle-translate -o jsonpath='{.spec.clusterIP}'):8083/metrics 2>/dev/null | grep subtitle_translate_ | head   # с ноды/пода; локально через kubectl port-forward
```

Expected: первый ответ 200 `0/N` `no-store`; HEAD растёт; финал `100/100` `public, max-age=86400` с португальским текстом; в метриках токены и один `translation finished`. Оценить стоимость: `tokens_input/output` × цена модели.

- [ ] **Step 6: Коммит infra**

```bash
cd /Users/vintikzzzz/Projects/webtor/infra/helmfile && git add charts/subtitle-translate values/subtitle-translate.yaml.gotmpl helmfile.yaml values/torrent-http-proxy/services.yaml environments/default/images.yaml && git commit -m "subtitle-translate: chart, release, ~tr routing" && git push origin main
```

Символьные ссылки в репо сервиса (по конвенции новых репо): `ln -s ../infra/helmfile/charts/subtitle-translate chart; ln -s ../infra/helmfile/values/subtitle-translate.yaml.gotmpl subtitle-translate.yaml.gotmpl` — оба уже в `.gitignore`.

---

## Порядок и зависимости

1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9. Task 4 можно делать параллельно с 5–6 (общий интерфейс из Task 3). Task 9 после Task 8 (нужен образ).

## Что сознательно не делается

- Дедупликация по содержимому исходника (один файл в разных раздачах).
- Квоты и учёт стоимости на пользователя (единый AI-лимит, отдельная спека).
- Проверка прав в сервисе: гейт в web-ui (план B).
- Стриминг ответа модели: батч целиком, прогрессивность на уровне батчей.
