MODULE := github.com/TechAlkurn/core

# Commit + push to main, then publish that exact commit to the Go proxy.
#
# proxy.golang.org caches `@latest` for a while, so right after a push
# `go get $(MODULE)@latest` / `go mod tidy` in an app can still resolve an
# older commit ("module ... found, but does not contain package ..."). Asking
# the proxy for the pushed commit hash makes it fetch that commit now; apps
# then pin it with the printed `go get` line.
up:
	@if [ -z "$(m)" ]; then \
		m="update"; \
	else \
		m="$(m)"; \
	fi; \
	git add .; \
	git commit -m "$$m"; \
	git push origin main && $(MAKE) --no-print-directory publish

# Warm the Go proxy with the current HEAD and print the line to run in apps.
# Safe to re-run on its own (e.g. if the proxy request failed after a push).
publish:
	@hash=$$(git rev-parse --short=12 HEAD); \
	echo "Publishing $(MODULE)@$$hash to proxy.golang.org ..."; \
	if GOPROXY=https://proxy.golang.org GOFLAGS=-mod=mod go list -m "$(MODULE)@$$hash"; then \
		echo ""; \
		echo "Update apps with:"; \
		echo "  go get $(MODULE)@$$hash && go mod tidy"; \
	else \
		echo "Proxy fetch failed — is HEAD pushed to origin/main?" >&2; \
		exit 1; \
	fi

.PHONY: up publish
