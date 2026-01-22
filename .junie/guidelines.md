Project development guidelines (advanced)

Documentation
- **Before starting work on any feature or component, always check the docs/ directory first**
  - The docs/ folder contains comprehensive technical specifications and architecture documentation
  - Each major feature has its own documentation file (e.g., docs/vault.md for the Vault system)
  - Documentation includes:
    - Database schemas and data models
    - Business logic and workflows
    - API specifications and integration points
    - Constraints, rules, and edge cases
  - Reading the relevant documentation before implementation helps avoid mistakes and ensures consistency with the existing architecture
  - If documentation is missing or outdated for a feature you're working on, consider updating it as part of your work
- **After completing any task, always update the relevant documentation in docs/ directory**
  - When adding new methods, functions, or services, document their signatures, parameters, algorithms, and error handling
  - When modifying existing functionality, update the corresponding documentation to reflect the changes
  - When adding new database tables or models, update the schema documentation and model descriptions
  - Documentation updates are a mandatory part of task completion, not an optional step

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
    - Stremio addon HTTP client: STREMIO_ADDON_USER_AGENT (custom user agent for addon requests), STREMIO_ADDON_PROXY (proxy URL for addon HTTP client, supports http:// and socks5:// schemes). Flags: --stremio-addon-user-agent, --stremio-addon-proxy (services/stremio).

- Database and migrations
  - PostgreSQL configuration flags are registered by github.com/webtor-io/common-services (RegisterPGFlags). On server start (serve.go), migrations in migrations/ are applied automatically. Ensure DB connectivity is configured via the common services environment variables (see common-services docs; typical PG_HOST/PG_PORT/PG_USER/PG_PASSWORD/PG_DATABASE and SSL flags).
  - A Redis client is also configured via common-services and used by the Job queues.

- Migration file conventions and style
  - **File naming**: Use sequential numbering format: `{number}_{description}.{up|down}.sql` (e.g., `19_create_stremio_settings.up.sql`).
  - **Schema qualification**: Always use explicit `public.` schema prefix for all table references.
  - **Indentation**: Use tab characters for indentation, not spaces.
  - **Data types**: Use lowercase PostgreSQL data types (`uuid`, `text`, `jsonb`, `timestamptz`).
  - **Column definitions**: Each column on its own line with consistent formatting:
    ```sql
    CREATE TABLE public.table_name (
    	column_id uuid DEFAULT uuid_generate_v4() NOT NULL,
    	user_id uuid NOT NULL,
    	data jsonb NOT NULL,
    	created_at timestamptz DEFAULT now() NOT NULL,
    	updated_at timestamptz DEFAULT now() NOT NULL,
    ```
  - **Constraint naming**: Use explicit constraint names following the pattern:
    - Primary key: `{table}_pk`
    - Unique constraints: `{table}_{column}_unique`
    - Foreign keys: `{table}_{reference}_fk`
  - **Foreign key references**: Format foreign key constraints with line breaks for readability:
    ```sql
    CONSTRAINT table_user_fk FOREIGN KEY (user_id)
    	REFERENCES public."user" (user_id) ON DELETE CASCADE
    ```
  - **Automatic timestamps**: Include `update_updated_at` trigger for tables with `updated_at` columns:
    ```sql
    create trigger update_updated_at before
    update
        on
        public.table_name for each row execute function update_updated_at();
    ```
  - **Down migrations**: Keep down migrations simple, typically just `DROP TABLE IF EXISTS table_name;`
  - **Consistency**: Follow the established patterns from existing migrations like `18_addon_url.up.sql` for consistent formatting across the project.

- Database operations architecture
  - **ALL database operations must be placed in the models/ directory**. This includes CRUD operations, queries, and any database-related business logic.
  - Handlers should NEVER contain direct database queries. Instead, they should call model methods that encapsulate the database operations.
  - Model files should be named after the primary entity they manage (e.g., models/embed_domain.go for EmbedDomain operations).
  - Each model should provide methods for common operations:
    - Get/List methods for retrieval (e.g., GetUserDomains, GetByID)
    - Create methods for insertion (e.g., CreateDomain)
    - Update methods for modifications (e.g., UpdateDomain)
    - Delete methods for removal (e.g., DeleteUserDomain)
    - Count/Exists methods for validation (e.g., CountUserDomains, DomainExists)
  - Model methods should accept a *pg.DB instance as their first parameter and return appropriate types with error handling.
  - This separation ensures better testability, maintainability, and follows the single responsibility principle.

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

Frontend Development Philosophy
- Server-side rendering priority
  - **ALWAYS prioritize server-side rendering over client-side JavaScript**. This project follows a server-side rendering approach for better performance, SEO, and user experience.
  - Use Go templates with Gin to render HTML on the server. Templates are located in templates/ directory and managed via TemplateManager.
  - JavaScript should only be used for essential interactive features that cannot be achieved with server-side rendering.

- Successful patterns to follow
  - **Stremio integration** (templates/partials/profile/stremio.html): Uses server-side forms with POST requests and redirects. Minimal JavaScript only for clipboard functionality.
  - **WebDAV integration** (templates/partials/profile/webdav.html): Similar pattern with form submissions and server-side processing.
  - **Embed domains** (templates/partials/profile/embed_domains.html): Refactored from JavaScript-heavy AJAX to server-side forms with data-async attributes for progressive enhancement.

- Implementation guidelines
  - Use HTML forms with method="post" for data mutations (create, update, delete operations).
  - Leverage data-async attributes for progressive enhancement without breaking core functionality.
  - Use `data-async-target` and `data-async-push-state="false"` for partial page updates when needed.
  - Include `X-Return-Url` header handling in handlers to redirect back to the originating page after form submissions.
  - Handlers should use `c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))` pattern for form processing.

- Anti-patterns to avoid
  - **Heavy JavaScript frameworks**: Avoid React, Vue, Angular, or similar client-side frameworks.
  - **AJAX-heavy interfaces**: Don't build interfaces that depend entirely on JavaScript for basic functionality.
  - **JSON APIs for simple CRUD**: Use server-side form processing instead of JSON APIs for basic create/read/update/delete operations.
  - **Client-side state management**: Keep application state on the server, not in JavaScript variables.

- When JavaScript is acceptable
  - Clipboard functionality (copying URLs, tokens) - see Stremio/WebDAV examples.
  - Progressive enhancement of existing server-side functionality.
  - Interactive features that genuinely improve UX without breaking core functionality.
  - Analytics tracking (Umami integration).

- Template organization
  - Place reusable components in templates/partials/
  - Use the existing template helper system for common functionality (web, umami, geoip helpers).
  - Follow the established pattern of data-async-layout for progressive enhancement.
  - Ensure templates work without JavaScript for core functionality.

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

- Interface naming conventions
  - **Interface names should NOT use "Interface" suffix**. Name interfaces directly, e.g., `StreamService`, not `StreamServiceInterface`.
  - **Implementation struct names should be descriptive**. Use specific names that describe the implementation type, e.g., `HttpStreamService` for HTTP-based implementations.
  - All interface methods should be well-documented with clear purpose and parameters.

- Logging practices
  - **Use global logrus instance** instead of injecting loggers into service constructors. Import `log "github.com/sirupsen/logrus"` and use `log.WithError()`, `log.WithField()`, etc.
  - **Include contextual information in logs**: Always log relevant context such as request URLs, service names, and other identifying information.
  - **Structured logging fields**: Use `WithField()` and `WithFields()` to add structured data to log entries for better searchability.
  - **Error handling**: Log errors with appropriate levels (Warn for recoverable errors, Error for serious issues) and include the original error using `WithError()`.
  - **All log messages should start with a small letter**
  - Example pattern for service logging:
    ```go
    log.WithError(err).
        WithField("service_name", serviceName).
        WithField("request_url", requestURL).
        Warn("service request failed, dropping results")
    ```

- Error handling and propagation
  - **Log errors only at the highest possible level**: Errors should be logged only once, at the point where they are finally handled (typically in HTTP handlers or main service entry points). This prevents log spam and makes debugging easier.
  - **Propagate errors up the call stack**: Lower-level functions (services, models, utilities) should return errors without logging them. Use `errors.Wrap()` from `github.com/pkg/errors` to add context as errors bubble up.
  - **Wrap errors with context at each level**: When returning an error from a function, wrap it with additional context about what operation failed:
    ```go
    result, err := someOperation()
    if err != nil {
        return errors.Wrap(err, "failed to perform some operation")
    }
    ```
  - **Log at the handler level**: HTTP handlers and main service entry points should log errors before returning responses:
    ```go
    result, err := service.DoSomething(ctx)
    if err != nil {
        log.WithError(err).
            WithField("operation", "do_something").
            Error("operation failed")
        c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
        return
    }
    ```
  - **Avoid intermediate logging**: Don't log the same error multiple times as it propagates up. Each layer should either:
    1. Wrap the error and return it (most common)
    2. Log the error and handle it (only at the top level)
  - **Exception for non-fatal errors**: If an error is caught and handled without propagating (e.g., optional feature failures, graceful degradation), it may be appropriate to log it at that level with Warn level.

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
