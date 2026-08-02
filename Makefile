build:
	npm run build \
	&& go build .

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
	go test -ldflags '$(PROTO_CONFLICT_LDFLAGS)' ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

clean:
	rm -rf web-ui assets/dist