# core

Shared Go library for the TechAlkurn services: a Gin REST gateway in front of an internal gRPC server, backed by MySQL through GORM. It holds the request/response glue, auth, a generic data layer, startup wiring and reusable components, so each app only adds its own protos, models and handlers.

```bash
go get github.com/TechAlkurn/core
```

Requires Go 1.26+.

## Packages

| Package                                                       | What it does                                                                                                                                                  |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `action`                                                      | Gin response envelope (`{status, message, token, data}`) and query-string `Context`                                                                           |
| `register`                                                    | Generic gRPC ↔ Gin bridge: builds gRPC requests from `gin.Context`, writes responses, gRPC server interceptors                                                |
| `reqctx`                                                      | Per-request identity (user id, lang) and current gRPC request                                                                                                 |
| `config`                                                      | Startup wiring: `.env`, DB, Redis, AWS, logging, Swagger, gRPC client/server, gRPC auth, rate limit, pprof, DB backup/restore                                 |
| `db`, `db/activerecord`, `db/hooks`, `db/drivers`, `db/query` | GORM connection registry, generic `ActiveRecord[T]`, model hooks, MySQL driver + schema introspection, raw-SQL helpers                                        |
| `components/base`                                             | Generic CRUD services (`Search`, `View`, `Del`) and `BaseForm[T]`                                                                                             |
| `components/middleware`                                       | JWT auth (frontend/backend), RBAC, recovery, timeouts, concurrency limit, Prometheus metrics                                                                  |
| `components/rbac`                                             | Port of Yii2 `DbManager` RBAC (same table schema)                                                                                                             |
| `components/*`                                                | `authorization` (JWT/PASETO), `gateway` (Stripe), `mailers`, `fcm`, `sms`, `socket`, `agora`, `uploader`, `image`, `calendar`, `cache`, `mutex`, `attempt`, … |
| `cons`                                                        | Shared constants and error messages (mirror of the host's `pkg/enum`)                                                                                         |
| `lib`                                                         | PHP-style helpers, type conversion, JWT, encryption, `DateTime`, `.ics` generation                                                                            |

The library never imports a host app. Host types are accepted through interfaces and generics, so your protoc-generated `*gen.Request` / `*gen.Response` plug in directly.

## Usage

### Startup

```go
import (
	"github.com/TechAlkurn/core/config"
	"github.com/TechAlkurn/core/db"
	"github.com/TechAlkurn/core/db/drivers"
)

func main() {
	config.InitEnv(rootDir) // loads .env (.env.local / .env.staging if present)

	driver := drivers.NewMysql() // reads DB_NAME, DB_URL
	db.SetConnection(driver.DB()) // required: db.Connection() panics otherwise
	db.SetLogger(logger)          // optional; any Printf-style logger
	// ...
}
```

### REST → gRPC handlers

```go
import "github.com/TechAlkurn/core/register"

var act = register.NewGrpcAction[*gen.Request, *gen.Response]()

func userHandler(c *gin.Context, ctx context.Context) {
	in := act.Request(c) // query, {"form": {...}} body, meta (method, ip, ua, source)
	act.HandleMethods(c, ctx, act.ActionMapper(register.GrpcHandlers[*gen.Request, *gen.Response]{
		{Keys: []string{"GET"}, Handler: act.HandleGrpc(client.UserList)},
		{Keys: []string{"POST", "PUT"}, Handler: act.HandleGrpc(client.UserSave)},
	}), in)
}
```

Request bodies are expected as `{"form": {...}}`. Responses are written as:

```json
{ "status": 200, "message": "success", "token": "", "data": [] }
```

On the gRPC server, register the interceptors so each RPC sees the caller's identity:

```go
grpc.NewServer(
	grpc.ChainUnaryInterceptor(config.GrpcAuthServerUnary, act.RecoveryInterceptor),
	grpc.ChainStreamInterceptor(config.GrpcAuthServerStream, act.StreamContextInterceptor),
)
```

and the client interceptors on the REST side (`config.GrpcAuthClientUnary` / `GrpcAuthClientStream`).

### Request identity

The REST middleware verifies the JWT and forwards the user to gRPC with a shared-secret header (`x-internal-auth`). Inside a handler:

```go
import "github.com/TechAlkurn/core/reqctx"

uid := reqctx.UserID()
lang := reqctx.Lang()
if !reqctx.IsOwner(item.UserId) { /* ... */ }

reqctx.Go(func() { /* background work keeps the identity */ })
```

The identity is bound to the handler goroutine and is **not** visible in a plain `go func()`, so use `reqctx.Go` instead.

### Services and forms

```go
import "github.com/TechAlkurn/core/components/base"

// list with filtering + pagination from the query string
page := base.Search(in, func(item *model.Post) any { return item })

// single item / delete by primary_id
item, err := base.View[model.Post](in, nil)
ok, err := base.Del[model.Post](in)

// create / update
frm := base.NewBaseForm[model.Post](in)
saved, err := frm.SaveItem()

// typed reads of the JSON body
title := base.ToMapped(in.GetForm()).ToString("title")
```

`in` is anything with `GetQuery() string` and `GetForm() []byte`, such as a generated request or `&base.Request{Query: "...", Form: body}` in cron jobs and tests.

### Active record

```go
import "github.com/TechAlkurn/core/db/activerecord"

ar := activerecord.NewAR[model.Post]()
post, err := ar.FindOne("id = ?", id)
posts, err := ar.FindAll("user_id = ?", uid)
n, err := ar.Count("status = ?", 1)

err = activerecord.Transaction(func(tx *gorm.DB) error {
	_, err := ar.Create(attrs, tx)
	return err
})
```

Model hooks: `hooks.RegisterTimestampHooks[model.Post]()`, `hooks.RegisterHookSave[model.Post](fn)`, etc.

### In-memory cache

```go
import "github.com/TechAlkurn/core/components/cache"

c := cache.NewRWMutexCache()
c.Set("k", v)
v := c.GetDefault("k", fallback)
```

## Configuration

Everything is read from environment variables when it's used, so loading a `.env` at startup is enough.

| Variable                                               | Used for                                                          |
| ------------------------------------------------------ | ----------------------------------------------------------------- |
| `SECRET_KEY`                                           | JWT signing; also derives the internal gRPC auth token            |
| `TOKEN_TTL` / `TOKEN_MAXAGE`                           | Token lifetime in seconds (`lib` / `components/authorization`)    |
| `ENCRYPTION_KEY`                                       | `lib.Encrypt` / `Decrypt` (AES-256-GCM). Required for `Encrypt`   |
| `PASETO_KEY`                                           | PASETO tokens (hex)                                               |
| `DB_NAME`, `DB_URL`                                    | MySQL connection                                                  |
| `REDIS_ADDR`                                           | Redis                                                             |
| `GRPC_HOST`, `GRPC_PORT`, `GRPC_TARGETS`               | gRPC server/client addresses                                      |
| `GRPC_AUTH_TOKEN`                                      | Internal gRPC shared secret (defaults to an HMAC of `SECRET_KEY`) |
| `GRPC_REFLECTION`                                      | Enable gRPC reflection                                            |
| `SITE_NAME`, `SITE_MODE`, `API_ENDPOINT`, `ROOT`       | App metadata and paths                                            |
| `S3_*`, `STRIPE_*`, `MJ_APIKEY_*`, `GITLAB_PROJECT_ID` | Component-specific integrations                                   |

## Development

```bash
go build ./...
go vet ./...
go test ./...
go test ./register -run TestName
go generate ./register/...   # regenerate test-only protos (needs protoc)
```

Keep exported signatures stable, because every consuming app builds against them.

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
