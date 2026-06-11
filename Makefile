.PHONY: build test run init

build:
	go build -o dist/good-morning ./cmd/good-morning

test:
	go test ./...

run: build
	./dist/good-morning run

run-dry: build
	./dist/good-morning run --dry-run

init: build
	./dist/good-morning init
