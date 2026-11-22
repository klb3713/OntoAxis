# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ontoaxis-core ./cmd/ontoaxis-core

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S ontoaxis \
    && adduser -S -G ontoaxis ontoaxis
COPY --from=build /out/ontoaxis-core /usr/local/bin/ontoaxis-core
USER ontoaxis
EXPOSE 8080 9090
HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/ontoaxis-core"]
