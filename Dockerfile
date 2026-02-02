FROM alpine:latest as certs

# getting certs
RUN apk update && apk upgrade && apk add --no-cache ca-certificates

FROM node:22 as build_assets

WORKDIR /app

COPY . .

RUN npm install

RUN npm run build

FROM golang:latest as build

# set work dir
WORKDIR /app

# copy the source files
COPY . .

# disable crosscompiling
ENV CGO_ENABLED=0

# compile linux only
ENV GOOS=linux

# build the binary with debug information removed
RUN go build -ldflags '-w -s -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore' -a -installsuffix cgo -o server

FROM alpine:latest

# set work dir
WORKDIR /app

# copy our static linked library
COPY --from=build /app/server .
# copy templates
COPY --from=build /app/templates ./templates
# copy migrations
COPY --from=build /app/migrations ./migrations
# copy pub
COPY --from=build /app/pub ./pub
# copy assets
COPY --from=build_assets /app/assets/dist ./assets/dist

# copy certs
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# tell we are exposing our service on ports 8080 8081
EXPOSE 8080 8081

ENV GIN_MODE=release

# run it!
ENTRYPOINT ["./server"]

CMD ["serve"]
