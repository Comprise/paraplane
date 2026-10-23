VERSION ?= dev
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: test vet-cross lint dist tree

test: vet-cross
	go test ./...

# vet-cross type-checks every package, tests included, for the other release
# OSes: OS-specific test code must not break the windows or darwin build.
vet-cross:
	GOOS=windows go vet ./...
	GOOS=darwin go vet ./...

# lint: the local part of the CI lint job (tools are fetched at the pinned
# versions; shellcheck must be installed).
lint:
	go mod tidy -diff
	go run honnef.co/go/tools/cmd/staticcheck@2026.2.2 ./...
	shellcheck -s sh bin/paraplane
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7

dist:
	@for p in $(PLATFORMS); do \
		GOOS=$${p%/*}; GOARCH=$${p#*/}; EXT=; \
		if [ "$$GOOS" = windows ]; then EXT=.exe; fi; \
		echo "building $$GOOS/$$GOARCH"; \
		CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o libexec/paraplane-$$GOOS-$$GOARCH$$EXT ./cmd/paraplane || exit 1; \
	done

# tree DIR=<dir>: copies what ships (ship.txt, the single list CI and the
# release read too) into DIR; libexec is skipped when it was not built.
tree:
	@if [ -z "$(DIR)" ]; then echo "usage: make tree DIR=<dir>" >&2; exit 2; fi
	@mkdir -p "$(DIR)"
	@while read -r p; do \
		[ -n "$$p" ] || continue; \
		if [ "$$p" = libexec ] && [ ! -e libexec ]; then continue; fi; \
		cp -R "$$p" "$(DIR)/" || exit 1; \
	done < ship.txt
