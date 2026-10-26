# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`github.com/TechAlkurn/core` is a shared Go library (no `main` package) consumed by other services via `go get github.com/TechAlkurn/core`. Changes here affect every downstream app, so keep exported function signatures stable.

## Commands

```bash
go build ./lib/... ./cache/...   # builds cleanly
go vet ./lib/... ./cache/...
go test ./...                    # there are currently no _test.go files
go test ./lib -run TestName      # single test, once tests exist
```

`go build ./...` **fails by design** in this repo: `action/response.go` imports `app/pkg/protos/gen`, a package that only exists in the consuming application's module (it provides the gRPC `gen.Response` type with `GetStatus()` / `Data []byte`). The `action` package therefore only compiles inside a host app. Don't "fix" this by removing the import without being asked.

`make up m="commit message"` runs `git add . && git commit && git push origin main` — it pushes directly to the remote. Don't run it unless the user asks.

## Architecture

Three packages:

- **`action/`** — Gin HTTP glue for a gateway that fronts gRPC services.
  - `request.go`: `Context` wraps a raw query string in an `*http.Request` so query params can be read/mutated (`Query`, `SetQueryParam`, `IgnoreNotification` checks `is_ignore`). `RawQuery` is assigned verbatim on purpose to preserve param order.
  - `response.go`: `Gin.Response(*gen.Response)` turns a gRPC response into the standard JSON envelope `BaseResponse{status, message, token, data}`. The proto `Data` field is JSON bytes; top-level `token` and `message` keys are lifted out of `data` into the envelope. Empty data becomes `[]`. `Abort`/`Failed` strip the gRPC `"desc = "` prefix from error messages. Includes `SafeResponseMiddleware` guarding against double writes.
- **`cache/`** — in-memory key/value stores. `NewMutexCache` and `NewRWMutexCache` share Get/Set/Delete/Has/Keys/Values/Len/Range/Flush; the RWMutex one adds `GetDefault`, `GetOrSet*` variants, etc. Constructors return unexported types.
- **`lib/`** — PHP-style helper functions (`InArray`, `Explode`, `Implode`, `Strtotime`, `Empty`, `Slugify`, `ToUint32`/`ToString` loose type conversions, etc.). Notable pieces:
  - `jwt.go` (golang-jwt **v5**): HS256 tokens. `ValidateJWT`/`LoggedUser` read the `id` claim and stash it in a **package-global** mutex map (`SetMuStorage`/`GetLoggedId`/`IsOwner`) — this is process-wide state, not per-request. Two generators both write the `id` claim: `GenerateJWT` (`exp` from `TOKEN_TTL` seconds; no expiry if unset/0) and `JwtGenerate` (5-year `exp`). For old tokens, the validator falls back to the legacy `logged_id` claim.
  - `security.go`: bcrypt passwords, generic `GenerateToken`/`ValidateToken`, and `Encrypt`/`Decrypt`, which use AES-256-GCM (`nonce||ciphertext`) with a key derived from `ENCRYPTION_KEY`. `Encrypt` errors if that variable is unset. `Decrypt` falls back to the old AES-CFB format with a key written in the code (`legacyEncryptionKey`) so existing data still decrypts; never encrypt with that key. `SecurityEncrypt` produces `v1::<base64>`, and `SecurityDecrypt` reads it (and the old raw-bytes form).
  - `datetime.go`: `DateTime` type with JSON + `sql.Scanner`/`driver.Valuer` using layout `2006-01-02 15:04:05`.
  - `datatype.go`: `BindJSON`/`ShouldBindJSON` Gin helpers expect the request body wrapped as `{"form": {...}}` and return only the inner `form` object (`ShouldBindJSON` also rejects non-POST/PUT/PATCH and merges extra maps in). Plus pointer helpers (`lib.String`, `lib.Int`, …).
  - `image.go`: `Thumb` generates local thumbnails (paths from env `cache`, `image`, `storage`, `storagePath`). The S3 resize/upload code is commented out; `ThumbS3`/`GenerateThumb` only compute the cache-key path.
  - `ics/`: iCalendar (`.ics`) event generation from text templates.

## Configuration

All config comes from environment variables: `SECRET_KEY`, `TOKEN_TTL`, `API_ENDPOINT`, `SITE_NAME`, `ENCRYPTION_KEY`, and the image path vars above. All are read at call time, so loading a `.env` in `main` works.

## Logging

Mixed loggers are in use: `action` uses a logrus `Log` but passes zap fields to it; `lib/errors.go` uses the grpc-filetransfer logger. Match whatever the file you're editing already uses.
