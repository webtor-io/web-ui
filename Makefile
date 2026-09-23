build:
	npm run build \
	&& go build .

# Regenerates docs/swagger from the annotations in handlers/api. The output is
# committed, so a normal build does not need the swag binary — run this after
# touching an endpoint's annotations.
#   go install github.com/swaggo/swag/cmd/swag@latest
swagger:
	swag init -g handlers/api/docs.go -d ./,./handlers/api,./services/libapi -o docs/swagger --instanceName libraryapi --parseDependency --parseDepth 2

run:
	./web-ui s

forward-ports:
	kubefwd svc -n webtor -l "app.kubernetes.io/name in (claims-provider, supertokens, rest-api, abuse-store)"

# The abuse-store and torrent-store protobufs both register
# "proto/torrent-store.proto", which panics at init and takes down every test
# binary that links both — a third of the packages here. The production build
# already passes this flag (see Dockerfile); tests need it for the same reason.
PROTO_CONFLICT_LDFLAGS := -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore

test:
	@docker info >/dev/null 2>&1 || echo "WARNING: docker is not available -- Postgres-backed tests will FAIL (they t.Fatal on the docker dial, they do not skip). A cached green result from an earlier run with docker up can hide that: re-run with -count=1 after docker state changes."
	go test -ldflags '$(PROTO_CONFLICT_LDFLAGS)' ./...

# Re-render the Stremio paywall clips (pub/stremio/paywall-<lang>.mp4) from the
# stremio.paywall.* locale keys, then run the test that checks them against the
# locales (docs/stremio.md, "The paywall clip"). Needs python3 and ffmpeg.
#   make paywall-clips
#   make paywall-clips ARGS="--lang ru --frames /tmp/paywall-frames"
#   make paywall-clips FFMPEG="docker run --rm -v $(CURDIR):$(CURDIR) -w $(CURDIR) jrottenberg/ffmpeg:8-alpine"
PAYWALL_VENV ?= /tmp/paywall-venv
paywall-clips:
	test -x $(PAYWALL_VENV)/bin/python || python3 -m venv $(PAYWALL_VENV)
	$(PAYWALL_VENV)/bin/pip install -q -r scripts/stremio_paywall_video/requirements.txt
	$(if $(FFMPEG),FFMPEG="$(FFMPEG)") $(PAYWALL_VENV)/bin/python scripts/stremio_paywall_video/render.py $(ARGS)
	go test -count=1 -ldflags '$(PROTO_CONFLICT_LDFLAGS)' ./handlers/stremio/ ./handlers/static/

vet:
	go vet ./...

fmt:
	go fmt ./...

clean:
	rm -rf web-ui assets/dist