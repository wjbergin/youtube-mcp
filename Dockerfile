# Build a static binary; distroless/static ships CA certs for Google API TLS.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /youtube-mcp .

FROM gcr.io/distroless/static-debian12
# auth.Dir resolves $HOME/.config/youtube-mcp; the deploy mounts the OAuth
# files there read-only. HOME is fixed regardless of the runtime UID.
ENV HOME=/home/app
COPY --from=build /youtube-mcp /usr/local/bin/youtube-mcp
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/youtube-mcp", "serve", "--http", ":8080"]
