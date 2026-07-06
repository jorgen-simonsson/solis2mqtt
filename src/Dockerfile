# Build context is the repository root (see docker-compose.yml), since the
# Go module (go.mod/go.sum) lives there alongside the existing tools/.
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/solis2mqtt ./src/cmd/solis2mqtt

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/solis2mqtt /app/solis2mqtt
ENTRYPOINT ["/app/solis2mqtt"]
CMD ["-config", "/app/config/config.json", "-env", "/app/.env"]
