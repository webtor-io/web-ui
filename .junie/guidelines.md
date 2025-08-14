Project development guidelines (advanced)

Build and configuration
- Toolchain versions
  - Go: go 1.24.x (module file declares 1.24.5). The project is a single Go module: github.com/webtor-io/web-ui.
  - Node: Node.js 22.x is used to build frontend assets (see Dockerfile stage "node:22").
  - Package managers: npm (package-lock.json is present). Yarn is not used.

- Build paths and outputs
  - Frontend assets are emitted to assets/dist by webpack (see webpack.config.js). Static assets are exposed by the server at /assets (handlers/static).
  - Public static files from pub are mounted at / and /pub.
  - Go server binary: web-ui during local go build; Docker build stage outputs /app/server for Alpine image.

- Makefile targets
  - make build: runs npm run build then go build .
  - make run: runs the locally built binary ./web-ui s (serve).
  - make forward-ports: uses kubefwd to forward selected services in the webtor namespace (claims-provider, supertokens, rest-api, abuse-store) for local development.

- Docker build
  - docker build . produces a minimal Alpine image. The build performs:
    1) npm install && npm run build in a Node 22 builder;
    2) go build in a golang:latest builder with CGO_ENABLED=0, GOOS=linux and additional -ldflags "-w -s -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore" to relax protobuf symbol conflicts;
    3) runtime image copies server, templates, migrations, pub, and built assets.
  - The container exposes ports 8080 and 8081 and runs ./server serve with GIN_MODE=release.

- Local development (hot reload)
  - npm start runs two processes with concurrently: "air" (Go live-reload) and webpack-dev-server.
  - air is expected to be installed on the developer machine. If missing, install via: go install github.com/cosmtrek/air@latest, then ensure GOPATH/bin is on PATH. An explicit .air.* config is not present; defaults are sufficient for this repo structure.

- Minimum runtime configuration
  - Web listener: WEB_HOST (default ""), WEB_PORT (default 8080). Flags: --host, --port (services/web).
  - REST API access (one of):
    - Direct Webtor REST-API: REST_API_SERVICE_HOST, REST_API_SERVICE_PORT (default 80), REST_API_SECURE (enable https), WEBTOR_API_KEY/WEBTOR_API_SECRET (if required by backend). Flags in services/api.
    - RapidAPI: RAPIDAPI_HOST and RAPIDAPI_KEY; when set, API calls are redirected via RapidAPI and HTTPS:443 is enforced.
  - Sessions: SESSION_SECRET (default "secret123"). Optional Redis-backed session store if REDIS_MASTER_SERVICE_HOST/REDIS_SERVICE_HOST and REDIS_MASTER_SERVICE_PORT/REDIS_SERVICE_PORT are provided; REDIS_PASS for password (handlers/session).
  - Static assets: ASSETS_PATH (default ./assets/dist), WEB_ASSETS_HOST (handlers/static). For production, ensure assets/dist exists (via npm run build).
  - Optional integrations (feature toggles and endpoints):
    - Umami analytics: USE_UMAMI, UMAMI_WEBSITE_ID, UMAMI_HOST_URL (services/umami).
    - GeoIP API cache: USE_GEOIP_API, GEOIP_API_SERVICE_HOST, GEOIP_API_SERVICE_PORT (services/geoip).
    - Claims-provider (user tiers/limits): USE_CLAIMS, CLAIMS_PROVIDER_SERVICE_HOST, CLAIMS_PROVIDER_SERVICE_PORT (services/claims). When enabled, claims are fetched via gRPC and cached with lazymap (1m success TTL, 10s error TTL).

- Database and migrations
  - PostgreSQL configuration flags are registered by github.com/webtor-io/common-services (RegisterPGFlags). On server start (serve.go), migrations in migrations/ are applied automatically. Ensure DB connectivity is configured via the common services environment variables (see common-services docs; typical PG_HOST/PG_PORT/PG_USER/PG_PASSWORD/PG_DATABASE and SSL flags).
  - A Redis client is also configured via common-services and used by the Job queues.

Running the server locally
- With local REST API or RapidAPI configured:
  1) npm install
  2) npm run build
  3) go build .
  4) ./web-ui serve \
       --host=127.0.0.1 --port=8080 \
       --webtor-rest-api-host=$REST_API_SERVICE_HOST \
       --webtor-rest-api-port=$REST_API_SERVICE_PORT \
       [--webtor-rest-api-secure] [--rapidapi-host=$RAPIDAPI_HOST --rapidapi-key=$RAPIDAPI_KEY]
- For dev hot reload: npm start (requires air). This starts webpack-dev-server and the Go server concurrently.

Testing
- Go tests
  - The repository includes a comprehensive unit test in services/parse_torrent_name (golden-file based). Run all tests:
    go test ./...
  - Run only the parser tests (verbose):
    go test ./services/parse_torrent_name -v
  - Update golden files when modifying the parser. The test accepts a -update flag to rewrite testdata/*.json to the new outputs:
    go test ./services/parse_torrent_name -run TestParser -update
    Note: Review diffs carefully before committing golden updates.

- Adding new tests (Go)
  - Place _test.go files alongside the package under test. Use package parsetorrentname (or <pkg>_test for black-box tests) and the standard testing package.
  - Example minimal test (we created and executed a variant of this locally to validate the flow; do not commit unless needed):
    package parsetorrentname
    import "testing"
    func TestParseSmoke(t *testing.T) {
        ti, err := Parse(&TorrentInfo{}, "Some.Movie.2012.1080p.BluRay.x264-Group")
        if err != nil { t.Fatalf("unexpected error: %v", err) }
        if ti.Year != 2012 { t.Fatalf("expected Year=2012, got %d", ti.Year) }
        if ti.Resolution != "1080p" { t.Fatalf("expected Resolution=1080p, got %q", ti.Resolution) }
        if ti.Quality != "BluRay" { t.Fatalf("expected Quality=BluRay, got %q", ti.Quality) }
    }
  - Execute it with:
    go test ./services/parse_torrent_name -run TestParseSmoke -v

- JavaScript tests
  - No JS test runner is configured. package.json defines "test" as a failing placeholder. Frontend is built via webpack; stylelint is present but not wired to npm scripts. If you add JS tests, introduce a runner (e.g., vitest/jest) and wire scripts, but keep bundle size constraints in mind (project favors lightweight frontend).

Additional development notes
- HTTP API client (services/api)
  - Builds X-Token JWT per request using WEBTOR_API_SECRET and adds X-Api-Key. When RAPIDAPI_* are set, switches to RapidAPI headers instead and uses HTTPS.
  - Client embeds request context values (remote IP, user agent, domain, session hash).
  - SSE parsing for /stats streams is implemented with a scanner; be cautious about context cancellation to avoid goroutine leaks.

- Claims and feature flags (services/claims)
  - When USE_CLAIMS is enabled, each HTTP request fetches user claims and injects them into the gin.Context. Helper functions:
    - claims.GetFromContext(c) -> *proto.GetResponse
    - claims.IsPaid middleware aborts with 402 when Tier.Id == 0.
  - For ad testing, setting a cookie test-ads or a query parameter test-ads forces Claims.Site.NoAds=false (useful for debugging ad rendering).

- Static/templating
  - Templates are composed via gin-contrib/multitemplate and a TemplateManager that injects helpers (web, umami, geoip). If you add templates, ensure they are registered via the TemplateManager before tm.Init().

- Jobs and queues
  - Job queues are backed by Redis (via common-services) with mode-dependent namespaces (gin.Mode). Ensure Redis connectivity in development if you exercise background jobs.

- Code style and linting
  - Go: use go fmt and go vet; repo uses logrus for logging and pkg/errors for wrapping.
  - Keep external calls time-bounded: most external APIs in this repo use context.WithTimeout and lazymap caches; follow the same patterns for new integrations.
  - Frontend: Tailwind v4 and webpack 5 are used. CSS is processed through postcss. Stylelint is available; to use it, add an npm script, e.g., "lint:css": "stylelint 'assets/src/**/*.css'".

Debugging tips
- Enable pprof/probe: serve.go registers common-services Pprof and Probe servers if corresponding flags/envs are set. Consult github.com/webtor-io/common-services for flag names and endpoints; typical pprof binding is on a secondary port.
- To test API connectivity without RapidAPI, port-forward rest-api from Kubernetes or set REST_API_SERVICE_HOST/PORT to a reachable instance. README shows kubectl and kubefwd examples.
- Asset path issues: If you run the binary outside the repo root, adjust --assets-path to point to the built assets dist directory or provide WEB_ASSETS_HOST to offload static files to a CDN.

Verified commands (executed during preparation)
- go test ./... completed successfully; the golden-file parser suite in services/parse_torrent_name passed.
- A smoke unit test for the parser was created, executed (go test ./services/parse_torrent_name -run TestParseSmoke -v), and removed as part of this documentation task.

Housekeeping
- Migrations run on startup; ensure DB is reachable before invoking ./web-ui serve to avoid boot failures.
- Avoid committing temporary test files used to validate flows described here. Keep this document as the single artifact of this task.
