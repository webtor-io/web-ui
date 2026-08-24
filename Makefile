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
	@docker info >/dev/null 2>&1 || echo "WARNING: docker is not available -- Postgres-backed tests will be SKIPPED, not run. The prune-partition test will not protect you."
	go test -ldflags '$(PROTO_CONFLICT_LDFLAGS)' ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

clean:
	rm -rf web-ui assets/dist