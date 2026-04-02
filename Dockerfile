FROM golang:1.25 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/cc-connect ./cmd/cc-connect

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /out/cc-connect /usr/local/bin/cc-connect

EXPOSE 9810 9820 9111

ENTRYPOINT ["/usr/local/bin/cc-connect"]
CMD ["-config", "/config/config.toml"]
