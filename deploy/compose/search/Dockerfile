FROM golang:1.26-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/ontoaxis-search ./cmd/ontoaxis-search

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/ontoaxis-search /usr/local/bin/ontoaxis-search
ENTRYPOINT ["/usr/local/bin/ontoaxis-search"]
CMD ["consume"]
