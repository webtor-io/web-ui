build:
	npm run build \
	&& go build .

run:
	./web-ui-v2 s

forward-ports:
	kubefwd svc -n webtor -l "app.kubernetes.io/name in (claims-provider, supertokens, rest-api, abuse-store)"