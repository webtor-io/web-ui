build:
	npm run build \
	&& go build .

run:
	./web-ui s

forward-ports:
	kubefwd svc -n webtor -l "app.kubernetes.io/name in (claims-provider, supertokens, rest-api, abuse-store)"

test:
	go test ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

clean:
	rm -rf web-ui assets/dist