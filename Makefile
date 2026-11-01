.PHONY: test build serve

test:
	go test -race ./...
	node --test internal/web/search_test.mjs

build:
	go build -o bin/search ./cmd/search

serve:
	go run ./cmd/search explore -index "$(SNAPSHOT)/index" -catalog "$(SNAPSHOT)" -addr 127.0.0.1:8100
