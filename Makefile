SHELL := /usr/bin/env bash

.PHONY: test fulltest bench cover fullcover cyclo fullcyclo

test:
	@go test -timeout 30s ./...

fulltest:
	@clear
	@go test -v -count=1 -timeout 30s ./...

bench:
	@go test -bench . -benchmem -run ^$$ -timeout 30s ./...

cover:
	@clear
	@tmp=$$(mktemp); \
	go test -coverprofile="$${tmp}" ./... && \
	go tool cover -func="$${tmp}"; \
	rm "$${tmp}"

fullcover:
	@mkdir -p .cover
	@rm -f .cover/*.cov .cover/coverage.txt .cover/coverage.html
	@set -o pipefail; \
	for pkg in $$(go list ./...); do \
		echo "-> $$pkg"; \
		outfile=".cover/$$(echo $$pkg | tr '/' '_' ).cov"; \
		go test -covermode=atomic -coverprofile="$$outfile" $$pkg || echo "   [tests failed] $$pkg (continuing)"; \
	done; \
	ls .cover/*.cov >/dev/null 2>&1 || { echo "No coverage profiles generated"; exit 1; }; \
	echo "mode: atomic" > .cover/coverage.txt; \
	tail -q -n +2 .cover/*.cov >> .cover/coverage.txt; \
	go tool cover -func=.cover/coverage.txt; \
	go tool cover -html=.cover/coverage.txt -o .cover/coverage.html; \

cyclo:
	@gocyclo -over 10 . || true

fullcyclo:
	@clear
	@gocyclo . || true
