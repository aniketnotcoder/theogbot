.PHONY: run build test tidy

run:
	go run ./cmd/theogbot

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/theogbot ./cmd/theogbot

test:
	go test ./...

tidy:
	go mod tidy
