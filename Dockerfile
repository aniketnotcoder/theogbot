FROM golang:1.25-alpine AS build
WORKDIR /src
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/theogbot ./cmd/theogbot

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
RUN mkdir -p /data && chown 10001:10001 /data
COPY --from=build /out/theogbot /usr/local/bin/theogbot
ENV STORAGE_DB_PATH=/data/theogbot.db
USER 10001:10001
ENTRYPOINT ["theogbot"]
