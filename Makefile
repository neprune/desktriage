.PHONY: build clean stop start restart test

build:
	go build -o desktriage ./cmd/desktriage

clean:
	rm -f desktriage

test:
	go test ./...
