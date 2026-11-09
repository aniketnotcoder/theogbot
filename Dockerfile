FROM golang:1.25-alpine AS build
WORKDIR /src
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/theogbot ./cmd/theogbot

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata su-exec
WORKDIR /app
RUN mkdir -p /data && chown 10001:10001 /data
COPY --from=build /out/theogbot /usr/local/bin/theogbot
ENV STORAGE_DB_PATH=/data/theogbot.db
ENTRYPOINT ["/bin/sh", "-c", "chown -R 10001:10001 /data && chmod 0755 /data && exec su-exec 10001:10001 /usr/local/bin/theogbot"]
