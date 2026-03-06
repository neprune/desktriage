.PHONY: build clean test pull reload update

build:
	go build -o desktriage ./cmd/desktriage

clean:
	rm -f desktriage

test:
	go test ./...

pull:
	git pull

reload:
	pkill -SIGHUP desktriage

update:
	@OLD=$$(git rev-parse HEAD) && \
	git pull && \
	NEW=$$(git rev-parse HEAD) && \
	if [ "$$OLD" != "$$NEW" ]; then \
		$(MAKE) build reload; \
	else \
		echo "Already up to date, skipping build/reload."; \
	fi
