package register

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/TechAlkurn/core/config"
	"github.com/TechAlkurn/core/cons"
	"github.com/TechAlkurn/core/middleware"

	"github.com/TechAlkurn/core/reqctx"

	"github.com/TechAlkurn/core/db/activerecord"

	"github.com/TechAlkurn/core/action"
	"github.com/TechAlkurn/core/lib"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// RequestMessage is the gRPC request shape GrpcAction works with. The host's
// protoc-generated Req (message Request { bytes form; string query;
// uint32 primary_id; string identifier; uint32 user_id; repeated string roles;
// map<string,string> meta; }) satisfies it, so this library never imports the
// host's generated package. Fields are written through protoreflect by their
// proto names (form, query, meta, user_id).
type RequestMessage interface {
	proto.Message
	GetQuery() string
	GetMeta() map[string]string
}

// ResponseMessage is the gRPC response shape (status, message, token, data),
// satisfied by the host's *gen.Response.
type ResponseMessage interface {
	proto.Message
	action.ProtoResponse
}

type (
	CtxHandler     func(*gin.Context, context.Context)
	RouterHandlers []struct {
		Keys    []string
		Handler CtxHandler
	}
	// Handler is a gRPC server method: func(ctx, Req) (Res, error).
	Handler[Req RequestMessage, Res ResponseMessage]            func(context.Context, Req) (Res, error)
	GrpcRouterHandlers[Req RequestMessage, Res ResponseMessage] []struct {
		Keys    []string
		Handler func(context.Context, Req) (Res, error)
		Params  map[string]any
	}
	GrpcAction[Req RequestMessage, Res ResponseMessage] struct {
		h map[string]CtxHandler
	}
	GrpcHandler[Req RequestMessage, Res ResponseMessage] struct {
		Keys    []string
		Handler func(context.Context, Req) (Res, error)
	}
	GrpcHandlers[Req RequestMessage, Res ResponseMessage]  []GrpcHandler[Req, Res]
	grpcFn[In any, Res any]                                func(context.Context, *In, ...grpc.CallOption) (Res, error)
	grpcRn[Res any]                                        func(*gin.Context, context.Context) (Res, error)
	GrpcHandlerFn[Req RequestMessage, Res ResponseMessage] func(context.Context, Req, ...grpc.CallOption) (Res, error)
)

var HandlerFuncs = []gin.HandlerFunc{
	middleware.ConcurrencyLimiter(100),
	middleware.RequestTimeout(30 * time.Second),
}

// NewGrpcAction builds a GrpcAction for the host's generated message types:
//
//	register.NewGrpcAction[*gen.Request, *gen.Response]()
func NewGrpcAction[Req RequestMessage, Res ResponseMessage]() *GrpcAction[Req, Res] {
	return &GrpcAction[Req, Res]{h: make(map[string]CtxHandler)}
}

func HandleGrpc[In any, Res any](in *In, fn grpcFn[In, Res], methods ...string) grpcRn[Res] {
	return func(_ *gin.Context, ctx context.Context) (Res, error) {
		return fn(ctx, in)
	}
}

// -- Registration helper --
func (r *GrpcAction[Req, Res]) Register(keys ...string) func(CtxHandler) {
	return func(handler CtxHandler) {
		for _, key := range keys {
			r.h[key] = handler
		}
	}
}

// Wrap gRPC function without grpc.CallOption
func (r *GrpcAction[Req, Res]) HandleGrpc(fn GrpcHandlerFn[Req, Res], method ...string) func(context.Context, Req) (Res, error) {
	return func(ctx context.Context, in Req) (Res, error) {
		if len(method) > 0 && !r.IsMethod(in.GetMeta()["method"], method...) {
			var zero Res
			return zero, errors.New(cons.NoPermission)
		}
		return fn(ctx, in)
	}
}

func (r *GrpcAction[Req, Res]) H() map[string]CtxHandler {
	return r.h
}

// GrpcHandle wraps a gRPC handler func + request into Handle
func (r *GrpcAction[Req, Res]) GrpcHandle(c *gin.Context, ctx context.Context, fn func(context.Context, Req) (Res, error), in Req) {
	r.Handle(c, ctx, func(_ *gin.Context, ctx context.Context) (Res, error) {
		return fn(ctx, in)
	})
}

// Unified request handler with context + error handling
func (r *GrpcAction[Req, Res]) Handle(c *gin.Context, ctx context.Context, handler func(*gin.Context, context.Context) (Res, error), method ...string) {
	if len(method) > 0 && !r.IsMethod(c.Request.Method, method...) {
		r.Abort(c, cons.NoPermission)
		return
	}
	res, err := handler(c, ctx)
	if err != nil {
		r.Abort(c, err.Error())
		return
	}
	action.NewResponse(c).Response(res)
}

// Action mapper for handlers
func (r *GrpcAction[Req, Res]) ActionMapper(handlers GrpcHandlers[Req, Res]) map[string]func(context.Context, Req) (Res, error) {
	m := make(map[string]func(context.Context, Req) (Res, error))
	for _, h := range handlers {
		if lib.IsEmpty(h.Keys) || len(h.Keys) == 0 {
			m["*"] = h.Handler
			continue
		}
		for _, key := range h.Keys {
			m[key] = h.Handler
		}
	}

	return m
}

// Handle method-based mapping
func (r *GrpcAction[Req, Res]) HandleMethods(c *gin.Context, ctx context.Context,
	handlers map[string]func(context.Context, Req) (Res, error),
	in Req,
) {
	r.Handle(c, ctx, func(_ *gin.Context, ctx context.Context) (Res, error) {
		if handler, ok := handlers[c.Request.Method]; ok {
			return handler(ctx, in)
		}
		if handler, ok := handlers["*"]; ok {
			return handler(ctx, in)
		}
		return r.Response(gin.H{"message": cons.NoPermission})
	})
}

// Response builder
func (r *GrpcAction[Req, Res]) Response(data any) (Res, error) {
	res := newMessage[Res]()
	setInt32(res, "status", http.StatusOK)
	setString(res, "message", "success")
	if !lib.IsEmpty(data) {
		if b := lib.ToMarshal(data); !lib.IsEmpty(b) {
			setBytes(res, "data", b)
			if isDebug := activerecord.IsDebug(); !isDebug {
			}
		}
	}
	return res, nil
}

// Build gRPC request from gin.Context
func (r *GrpcAction[Req, Res]) Request(c *gin.Context, args ...map[string]any) Req {
	req := newMessage[Req]()
	setString(req, "query", c.Request.URL.RawQuery)
	// Skip JSON binding for multipart bodies: it consumes the start of the
	// body, so a later ParseMultipartForm silently drops the first file.
	if r.IsMethod(c.Request.Method, http.MethodPost, http.MethodPut, http.MethodPatch) &&
		!strings.HasPrefix(c.ContentType(), "multipart/") {
		if form, err := lib.ShouldBindJSON(c, args...); err == nil {
			setBytes(req, "form", form)
		}
	}
	source := "frontend"
	if _, file, _, ok := runtime.Caller(1); ok {
		if strings.Contains(file, "backend") {
			source = "backend"
		}
		if strings.Contains(file, "frontend") {
			source = "frontend"
		}
	}
	setStringMap(req, "meta", map[string]string{
		"method": c.Request.Method,
		"ip":     c.ClientIP(),
		"ua":     c.Request.UserAgent(),
		"source": source,
	})
	return req
}

func (r *GrpcAction[Req, Res]) IsBackend(in Req) bool {
	source, ok := in.GetMeta()["source"]
	return ok && source == "backend"
}

// IsPrimaryId
func (r *GrpcAction[Req, Res]) IsPrimary(params ...string) bool {
	if lib.NotEmpty(r.Query("primary_id")) {
		return true
	}
	if len(params) > 0 {
		for _, p := range params {
			pVal := r.Query(p)
			if lib.IsEmpty(pVal) {
				return false
			}
		}
		return true
	}
	return false
}

// IsKey
func (r *GrpcAction[Req, Res]) IsKey(key string) bool {
	return r.Query("key") == key
}

func (r *GrpcAction[Req, Res]) IsAdmin() bool {
	return reqctx.UserID() == cons.ADMIN_ID
}

func (r *GrpcAction[Req, Res]) IsOwner(userId uint32) bool {
	return reqctx.IsOwner(userId)
}

func (r *GrpcAction[Req, Res]) IgnoreNotification() bool {
	isIgnore := r.Query("is_ignore")
	switch isIgnore {
	case "1", "yes", "YES", "Yes":
		return true
	}
	return false
}

// Method helpers
func (r *GrpcAction[Req, Res]) IsMethod(method string, methods ...string) bool {
	return lib.InArray(method, methods)
}

func (r *GrpcAction[Req, Res]) IsPost(in Req) bool {
	method, ok := in.GetMeta()["method"]
	return ok && r.IsMethod(method, http.MethodPost, http.MethodPut, http.MethodPatch)
}

func (r *GrpcAction[Req, Res]) IsDelete(in Req) bool {
	method, ok := in.GetMeta()["method"]
	return ok && r.IsMethod(method, http.MethodDelete)
}

func (r *GrpcAction[Req, Res]) IsGet(in Req) bool {
	method, ok := in.GetMeta()["method"]
	return ok && r.IsMethod(method, http.MethodGet)
}

func (r *GrpcAction[Req, Res]) Abort(c *gin.Context, msg string) {
	action.NewResponse(c).Abort(errors.New(msg))
}

func (r *GrpcAction[Req, Res]) NoPermission(c *gin.Context) {
	r.Abort(c, cons.NoPermission)
}

// Recovery for HTTP handlers
func (r *GrpcAction[Req, Res]) Recover(c *gin.Context) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())
		log.Printf("HTTP Panic Recovered: %v\n%s", r, stack)
		go sendErrorAlert(r, stack)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}

	// Ensure cancellation is respected
	if ctxErr := c.Request.Context().Err(); ctxErr != nil && !c.Writer.Written() {
		c.AbortWithStatus(http.StatusRequestTimeout)
	}
}

// Request logging middleware
func (r *GrpcAction[Req, Res]) RequestLogger(c *gin.Context) {
	start := time.Now()
	reqID := uuid.New().String()

	config.Log.WithFields(logrus.Fields{
		"id":     reqID,
		"path":   c.FullPath(),
		"method": c.Request.Method,
		"query":  c.Request.URL.Query(),
	}).Info("Request started")

	defer func() {
		config.Log.WithFields(logrus.Fields{
			"id":       reqID,
			"duration": time.Since(start),
		}).Info("Request completed")
	}()
}

func (r *GrpcAction[Req, Res]) Wrap(fn func() (any, error)) (Res, error) {
	rs, err := fn()
	if err != nil {
		var zero Res
		return zero, err
	}
	return r.Response(rs)
}

// Query reads a query-param from the current request without needing the
// caller to keep a local `req := action.Request(in.Query)`. The request is
// bound to the handler goroutine by RecoveryInterceptor (see reqctx). Only
// valid on that goroutine — use reqctx.Go for background work.
func (r *GrpcAction[Req, Res]) _query(key string) string {
	in, ok := reqctx.Request().(Req)
	if !ok || !in.ProtoReflect().IsValid() {
		return ""
	}
	return action.Request(in.GetQuery()).Query(key)
}

func (r *GrpcAction[Req, Res]) Has(key string) bool {
	in, ok := reqctx.Request().(Req)
	if !ok || !in.ProtoReflect().IsValid() {
		return false
	}
	return action.Request(in.GetQuery()).HasQueryParam(key)
}

func (r *GrpcAction[Req, Res]) Query(key string) string {
	return r._query(key)
}

func (r *GrpcAction[Req, Res]) Uint8Query(key string) uint8 {
	return lib.ToUint8(r._query(key))
}

func (r *GrpcAction[Req, Res]) Uint16Query(key string) uint16 {
	return lib.ToUint16(r._query(key))
}

func (r *GrpcAction[Req, Res]) Uint32Query(key string) uint32 {
	return lib.ToUint32(r._query(key))
}

func (r *GrpcAction[Req, Res]) Uint64Query(key string) uint64 {
	return lib.ToUint64(r._query(key))
}

func (r *GrpcAction[Req, Res]) UintQuery(key string) uint {
	return lib.ToUint(r._query(key))
}

func (r *GrpcAction[Req, Res]) Int8Query(key string) int8 {
	return lib.ToInt8(r._query(key))
}

func (r *GrpcAction[Req, Res]) Int16Query(key string) int16 {
	return lib.ToInt16(r._query(key))
}

func (r *GrpcAction[Req, Res]) Int32Query(key string) int32 {
	return lib.ToInt32(r._query(key))
}

func (r *GrpcAction[Req, Res]) Int64Query(key string) int64 {
	return lib.ToInt64(r._query(key))
}

func (r *GrpcAction[Req, Res]) IntQuery(key string) int {
	return lib.ToInt(r._query(key))
}

func (r *GrpcAction[Req, Res]) Float32Query(key string) float32 {
	return lib.ToFloat32(r._query(key))
}

func (r *GrpcAction[Req, Res]) Float64Query(key string) float64 {
	return lib.ToFloat64(r._query(key))
}

func (r *GrpcAction[Req, Res]) BoolQuery(key string) bool {
	return lib.ToBool(r._query(key))
}

// RecoveryInterceptor for gRPC server. It also binds the caller's identity
// (forwarded by the REST layer, trusted because config.GrpcAuthServerUnary
// runs first) and the request to this goroutine for the length of the call.
func (r *GrpcAction[Req, Res]) RecoveryInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	id := config.GrpcIdentityFromIncoming(ctx)
	if in, ok := req.(Req); ok {
		setUint32(in, "user_id", id.UserID) // never trust a client-supplied value
	}
	defer reqctx.Bind(id, req)()
	ctx = reqctx.With(ctx, id)
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			log.Printf("gRPC Panic Recovered: %v\n%s", r, stack)
			go sendErrorAlert(r, stack)
			err = status.Errorf(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
}

// StreamContextInterceptor binds the caller's identity for streaming RPCs
// (e.g. Upload), the counterpart of RecoveryInterceptor's binding.
func (r *GrpcAction[Req, Res]) StreamContextInterceptor(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	id := config.GrpcIdentityFromIncoming(ss.Context())
	defer reqctx.Bind(id, nil)()
	return handler(srv, ss)
}

// ---- Helpers ----

// newMessage allocates an empty M (e.g. *gen.Request) from its proto type.
// Generated ProtoReflect methods accept a nil receiver, so the zero M is enough
// to reach the message type.
func newMessage[M proto.Message]() M {
	var zero M
	return zero.ProtoReflect().Type().New().Interface().(M)
}

// field returns m's reflection and the field named name when it exists with
// the given kind; fd is nil when m is nil or has no such field.
func field(m proto.Message, name string, kind protoreflect.Kind) (r protoreflect.Message, fd protoreflect.FieldDescriptor) {
	r = m.ProtoReflect()
	if !r.IsValid() {
		return nil, nil
	}
	fd = r.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil || fd.Kind() != kind || fd.IsList() {
		return nil, nil
	}
	return r, fd
}

func setString(m proto.Message, name, v string) {
	if r, fd := field(m, name, protoreflect.StringKind); fd != nil {
		r.Set(fd, protoreflect.ValueOfString(v))
	}
}

func setBytes(m proto.Message, name string, v []byte) {
	if r, fd := field(m, name, protoreflect.BytesKind); fd != nil {
		r.Set(fd, protoreflect.ValueOfBytes(v))
	}
}

func setInt32(m proto.Message, name string, v int32) {
	if r, fd := field(m, name, protoreflect.Int32Kind); fd != nil {
		r.Set(fd, protoreflect.ValueOfInt32(v))
	}
}

func setUint32(m proto.Message, name string, v uint32) {
	if r, fd := field(m, name, protoreflect.Uint32Kind); fd != nil {
		r.Set(fd, protoreflect.ValueOfUint32(v))
	}
}

// setStringMap replaces a map<string,string> field.
func setStringMap(m proto.Message, name string, v map[string]string) {
	r, fd := field(m, name, protoreflect.MessageKind)
	if fd == nil || !fd.IsMap() || fd.MapKey().Kind() != protoreflect.StringKind || fd.MapValue().Kind() != protoreflect.StringKind {
		return
	}
	r.Clear(fd)
	mp := r.Mutable(fd).Map()
	for k, val := range v {
		mp.Set(protoreflect.ValueOfString(k).MapKey(), protoreflect.ValueOfString(val))
	}
}

// Sends panic details to GitLab issues
func sendErrorAlert(recovered any, stack string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("sendErrorAlert panicked: %v\n", r)
		}
	}()
	_ = sendErrorToGitLab(fmt.Sprintf("*Panic recovered:*\n```\n%v\n\n%s\n```", recovered, stack))
}

func sendErrorToGitLab(description string) error {
	/*
		reporting := config.GetString("gitlab_error_reporting")
		if strings.ToUpper(reporting) != "YES" {
			return fmt.Errorf(cons.PermissionDenied)
		}
	*/
	token := config.GetString("gitlab_token")
	projectID := config.GetString("gitlab_project_id")
	if lib.IsEmpty(projectID) {
		projectID = os.Getenv("GITLAB_PROJECT_ID")
	}
	if token == "" || projectID == "" {
		return fmt.Errorf("missing GITLAB_TOKEN or GITLAB_PROJECT_ID")
	}
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/issues", projectID)
	form := url.Values{}
	form.Set("title", "Panic recovered by developer")
	form.Set("description", description)
	form.Set("labels", "bug")
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var body bytes.Buffer
		_, _ = body.ReadFrom(resp.Body)
		return fmt.Errorf("GitLab API error: %s - %s", resp.Status, body.String())
	}
	config.Log.Info("GitLab issue created successfully")
	return nil
}
