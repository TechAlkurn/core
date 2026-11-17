package config

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"os"
	"strconv"

	"github.com/TechAlkurn/core/reqctx"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// GrpcAuthHeader carries the shared secret that proves a gRPC call came from
// this application's own REST layer. The gRPC server is internal-only; this
// stops anything else that can reach the port from calling it.
const GrpcAuthHeader = "x-internal-auth"

// GrpcAuthToken returns GRPC_AUTH_TOKEN if set, otherwise an HMAC of
// SECRET_KEY (so existing deployments need no new env var, and the JWT signing
// key itself never goes on the wire). Empty when neither is configured.
var GrpcAuthToken = func() string {
	if token := os.Getenv("GRPC_AUTH_TOKEN"); token != "" {
		return token
	}
	secret := os.Getenv("SECRET_KEY")
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(os.Getenv("GRPC_INTERNAL_KEY")))
	return hex.EncodeToString(mac.Sum(nil))
}

// Identity headers forwarded from the REST layer. The gRPC server trusts them
// ONLY because checkGrpcAuth runs first: just our own REST layer holds the
// shared secret, and it sets these from the verified token, never from input.
const (
	grpcUserIDHeader   = "x-user-id"
	grpcUsernameHeader = "x-user-name"
	grpcEmailHeader    = "x-user-email"
	grpcLangHeader     = "x-lang"
)

func withGrpcAuth(ctx context.Context) context.Context {
	// Replace (not append) any identity already on ctx so nothing upstream can
	// smuggle in a second x-user-id.
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(GrpcAuthHeader, GrpcAuthToken())
	id, _ := reqctx.From(ctx)
	md.Set(grpcUserIDHeader, strconv.FormatUint(uint64(id.UserID), 10))
	md.Set(grpcUsernameHeader, id.Username)
	md.Set(grpcEmailHeader, id.Email)
	md.Set(grpcLangHeader, id.Lang)
	return metadata.NewOutgoingContext(ctx, md)
}

// GrpcIdentityFromIncoming reads the identity the REST layer forwarded. Call
// only after GrpcAuthServerUnary/Stream has accepted the call.
func GrpcIdentityFromIncoming(ctx context.Context) reqctx.Identity {
	md, _ := metadata.FromIncomingContext(ctx)
	first := func(key string) string {
		if v := md.Get(key); len(v) == 1 {
			return v[0]
		}
		return "" // missing or duplicated: treat as absent
	}
	userID, _ := strconv.ParseUint(first(grpcUserIDHeader), 10, 32)
	return reqctx.Identity{
		UserID:   uint32(userID),
		Username: first(grpcUsernameHeader),
		Email:    first(grpcEmailHeader),
		Lang:     first(grpcLangHeader),
	}
}

// GrpcAuthClientUnary / GrpcAuthClientStream attach the token to every call.
func GrpcAuthClientUnary(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	return invoker(withGrpcAuth(ctx), method, req, reply, cc, opts...)
}

func GrpcAuthClientStream(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return streamer(withGrpcAuth(ctx), desc, cc, method, opts...)
}

// checkGrpcAuth fails closed: with no token configured, every call is rejected.
func checkGrpcAuth(ctx context.Context) error {
	want := GrpcAuthToken()
	if want == "" {
		return status.Error(codes.Unauthenticated, "gRPC auth token not configured")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	got := md.Get(GrpcAuthHeader)
	if len(got) != 1 || subtle.ConstantTimeCompare([]byte(got[0]), []byte(want)) != 1 {
		return status.Error(codes.Unauthenticated, "unauthenticated")
	}
	return nil
}

// GrpcAuthServerUnary / GrpcAuthServerStream reject calls without a valid token.
func GrpcAuthServerUnary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if err := checkGrpcAuth(ctx); err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

func GrpcAuthServerStream(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := checkGrpcAuth(ss.Context()); err != nil {
		return err
	}
	return handler(srv, ss)
}

// GrpcListenAddr binds to loopback unless GRPC_HOST says otherwise; every
// GRPC_TARGETS in use points at localhost.
func GrpcListenAddr(port string) string {
	host := os.Getenv("GRPC_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	return host + ":" + port
}

// GrpcReflectionEnabled exposes the service list only in local/dev, or when
// GRPC_REFLECTION=true is set explicitly.
func GrpcReflectionEnabled() bool {
	if v := os.Getenv("GRPC_REFLECTION"); v != "" {
		return v == "true"
	}
	// mode := GetString("site_mode")
	// SITE_MODE is an env var (it picks the .env file), not a DB config row.
	// Reading it via GetString hits the DB — panics before DB init and in
	// tests — and the DB key is lowercase `site_mode` anyway.
	mode := os.Getenv("SITE_MODE")
	return mode == "local" || mode == "development"
}
