# core

```bash
go get github.com/TechAlkurn/core
```

## Publishing a change

```bash
make up m="commit message"   # commit + push to main, then publish to the Go proxy
make publish                 # re-publish current HEAD only (no commit/push)
```

After pushing, `make up` asks `proxy.golang.org` for the exact commit and
prints the line to run in each consuming app:

```bash
go get github.com/TechAlkurn/core@<commit-hash> && go mod tidy
```

Pin the commit hash rather than using `@latest`: the proxy caches `@latest`
for a while, so right after a push it can still return an older commit and
fail with `module ... found, but does not contain package ...`.

Alternatively, skip the proxy for this module entirely (one-time, per machine):

```bash
go env -w GOPRIVATE=github.com/TechAlkurn/*
```
