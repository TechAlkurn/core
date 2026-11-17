package config

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// startServer runs an in-memory gRPC server wired exactly like main.go's
// RunGRPC (auth interceptors on unary + stream) and returns a dialer.
func startServer(t *testing.T) func(...grpc.DialOption) healthpb.HealthClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(GrpcAuthServerUnary),
		grpc.StreamInterceptor(GrpcAuthServerStream),
	)
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return func(opts ...grpc.DialOption) healthpb.HealthClient {
		opts = append(opts,
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		conn, err := grpc.NewClient("passthrough:///bufnet", opts...)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { conn.Close() })
		return healthpb.NewHealthClient(conn)
	}
}

func withAuth() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithUnaryInterceptor(GrpcAuthClientUnary),
		grpc.WithStreamInterceptor(GrpcAuthClientStream),
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func unaryCode(t *testing.T, c healthpb.HealthClient, cx context.Context) codes.Code {
	t.Helper()
	_, err := c.Check(cx, &healthpb.HealthCheckRequest{})
	return status.Code(err)
}

func streamCode(t *testing.T, c healthpb.HealthClient, cx context.Context) codes.Code {
	t.Helper()
	s, err := c.Watch(cx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return status.Code(err)
	}
	_, err = s.Recv()
	return status.Code(err)
}

// -----------------------------------------------------------------------------
// Server interceptors
// -----------------------------------------------------------------------------

func TestGrpcAuth_ClientWithTokenAllowed(t *testing.T) {
	t.Setenv("GRPC_AUTH_TOKEN", "")
	t.Setenv("SECRET_KEY", "test-secret")
	c := startServer(t)(withAuth()...)

	if code := unaryCode(t, c, ctx(t)); code != codes.OK {
		t.Errorf("unary: got %v want OK", code)
	}
	if code := streamCode(t, c, ctx(t)); code != codes.OK {
		t.Errorf("stream: got %v want OK", code)
	}
}

func TestGrpcAuth_NoTokenRejected(t *testing.T) {
	t.Setenv("SECRET_KEY", "test-secret")
	c := startServer(t)() // no client interceptors — like an outside caller

	if code := unaryCode(t, c, ctx(t)); code != codes.Unauthenticated {
		t.Errorf("unary: got %v want Unauthenticated", code)
	}
	if code := streamCode(t, c, ctx(t)); code != codes.Unauthenticated {
		t.Errorf("stream: got %v want Unauthenticated", code)
	}
}

func TestGrpcAuth_WrongTokenRejected(t *testing.T) {
	t.Setenv("SECRET_KEY", "test-secret")
	c := startServer(t)()

	bad := metadata.AppendToOutgoingContext(ctx(t), GrpcAuthHeader, "guess")
	if code := unaryCode(t, c, bad); code != codes.Unauthenticated {
		t.Errorf("unary: got %v want Unauthenticated", code)
	}
}

func TestGrpcAuth_DuplicateHeaderRejected(t *testing.T) {
	t.Setenv("SECRET_KEY", "test-secret")
	c := startServer(t)()

	dup := metadata.AppendToOutgoingContext(ctx(t), GrpcAuthHeader, "guess", GrpcAuthHeader, GrpcAuthToken())
	if code := unaryCode(t, c, dup); code != codes.Unauthenticated {
		t.Errorf("unary: got %v want Unauthenticated", code)
	}
}

func TestGrpcAuth_FailsClosedWithoutSecret(t *testing.T) {
	t.Setenv("GRPC_AUTH_TOKEN", "")
	t.Setenv("SECRET_KEY", "")
	c := startServer(t)(withAuth()...)

	// Even a client sending the (empty) token must be refused.
	if code := unaryCode(t, c, ctx(t)); code != codes.Unauthenticated {
		t.Errorf("unary: got %v want Unauthenticated", code)
	}
}

// -----------------------------------------------------------------------------
// GrpcAuthToken
// -----------------------------------------------------------------------------

func TestGrpcAuthToken_DerivedFromSecretKey(t *testing.T) {
	t.Setenv("GRPC_AUTH_TOKEN", "")
	t.Setenv("SECRET_KEY", "test-secret")
	tok := GrpcAuthToken()
	if len(tok) != 64 {
		t.Errorf("want 64-char hex HMAC, got %q", tok)
	}
	if tok == "test-secret" {
		t.Error("SECRET_KEY must not be sent as-is")
	}
	if GrpcAuthToken() != tok {
		t.Error("token must be stable for the same secret")
	}
	t.Setenv("SECRET_KEY", "other-secret")
	if GrpcAuthToken() == tok {
		t.Error("token must change with SECRET_KEY")
	}
}

func TestGrpcAuthToken_ExplicitOverride(t *testing.T) {
	t.Setenv("SECRET_KEY", "test-secret")
	t.Setenv("GRPC_AUTH_TOKEN", "explicit")
	if got := GrpcAuthToken(); got != "explicit" {
		t.Errorf("got %q want explicit", got)
	}
}

// -----------------------------------------------------------------------------
// Listen address and reflection
// -----------------------------------------------------------------------------

func TestGrpcListenAddr(t *testing.T) {
	t.Setenv("GRPC_HOST", "")
	if got := GrpcListenAddr("50050"); got != "127.0.0.1:50050" {
		t.Errorf("default: got %q want 127.0.0.1:50050", got)
	}
	t.Setenv("GRPC_HOST", "0.0.0.0")
	if got := GrpcListenAddr("50050"); got != "0.0.0.0:50050" {
		t.Errorf("override: got %q", got)
	}
}

func TestGrpcReflectionEnabled(t *testing.T) {
	cases := []struct {
		mode, flag string
		want       bool
	}{
		{"production", "", false},
		{"live", "", false},
		{"staging", "", false},
		{"local", "", true},
		{"development", "", true},
		{"production", "true", true},
		{"local", "false", false},
	}
	for _, tc := range cases {
		t.Setenv("SITE_MODE", tc.mode)
		t.Setenv("GRPC_REFLECTION", tc.flag)
		if got := GrpcReflectionEnabled(); got != tc.want {
			t.Errorf("SITE_MODE=%q GRPC_REFLECTION=%q: got %v want %v", tc.mode, tc.flag, got, tc.want)
		}
	}
}
