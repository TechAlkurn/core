package register

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TechAlkurn/core/config"

	"github.com/TechAlkurn/core/reqctx"

	"github.com/TechAlkurn/core/db/activerecord"
	gen "github.com/TechAlkurn/core/register/internal/testpb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

// testAction is GrpcAction instantiated with the test protos, as a host app
// instantiates it with its own *gen.Request / *gen.Response.
type testAction = GrpcAction[*gen.Request, *gen.Response]

func newAction() *testAction { return NewGrpcAction[*gen.Request, *gen.Response]() }

func init() {
	// GrpcAction.Response calls IsDebug, which otherwise queries the DB.
	activerecord.IsDebug = func() bool { return false }
}

// echoServer reports what the handler goroutine sees, via reqctx and Query.
type echoServer struct {
	gen.UnimplementedCoreServiceServer
	*testAction
}

func (s *echoServer) HealthCheck(ctx context.Context, in *gen.Request) (*gen.Response, error) {
	time.Sleep(time.Millisecond) // widen the race window
	fromCtx, _ := reqctx.From(ctx)
	msg := fmt.Sprintf("%d|%s|%d|%s|%d", reqctx.UserID(), reqctx.Lang(), in.UserId, s.Query("x"), fromCtx.UserID)
	return &gen.Response{Message: msg}, nil
}

func (s *echoServer) Upload(stream gen.CoreService_UploadServer) error {
	for {
		if _, err := stream.Recv(); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
	}
	return stream.SendAndClose(&gen.Response{Message: strconv.FormatUint(uint64(reqctx.UserID()), 10)})
}

// startCore wires the server exactly like main.go's RunGRPC, with only two
// stream workers so goroutines are reused across RPCs.
func startCore(t *testing.T) gen.CoreServiceClient {
	t.Helper()
	t.Setenv("SECRET_KEY", "test-secret")
	t.Setenv("GRPC_AUTH_TOKEN", "")

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(
		grpc.NumStreamWorkers(2),
		grpc.ChainUnaryInterceptor(config.GrpcAuthServerUnary, newAction().RecoveryInterceptor),
		grpc.ChainStreamInterceptor(config.GrpcAuthServerStream, newAction().StreamContextInterceptor),
	)
	gen.RegisterCoreServiceServer(srv, &echoServer{testAction: newAction()})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(config.GrpcAuthClientUnary),
		grpc.WithStreamInterceptor(config.GrpcAuthClientStream),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return gen.NewCoreServiceClient(conn)
}

type seen struct {
	user, inUser, ctxUser uint32
	lang, query           string
}

func check(t *testing.T, c gen.CoreServiceClient, ctx context.Context, in *gen.Request) seen {
	t.Helper()
	res, err := c.HealthCheck(ctx, in)
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	p := strings.Split(res.Message, "|")
	u, _ := strconv.ParseUint(p[0], 10, 32)
	iu, _ := strconv.ParseUint(p[2], 10, 32)
	cu, _ := strconv.ParseUint(p[4], 10, 32)
	return seen{user: uint32(u), lang: p[1], inUser: uint32(iu), query: p[3], ctxUser: uint32(cu)}
}

func as(user uint32, lang string) context.Context {
	return reqctx.With(context.Background(), reqctx.Identity{UserID: user, Lang: lang})
}

func TestIdentity_ForwardedToHandler(t *testing.T) {
	c := startCore(t)
	got := check(t, c, as(7, "ar"), &gen.Request{Query: "x=hello"})
	want := seen{user: 7, lang: "ar", inUser: 7, query: "hello", ctxUser: 7}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestIdentity_ClientCannotSetUserId(t *testing.T) {
	c := startCore(t)
	// A request body claiming user 999 must be overwritten by the verified id.
	if got := check(t, c, as(7, ""), &gen.Request{UserId: 999}); got.user != 7 || got.inUser != 7 {
		t.Errorf("logged-in: got %+v, want user 7", got)
	}
	if got := check(t, c, context.Background(), &gen.Request{UserId: 999}); got.user != 0 || got.inUser != 0 {
		t.Errorf("anonymous: got %+v, want user 0", got)
	}
}

func TestIdentity_UpstreamMetadataCannotSmuggleUser(t *testing.T) {
	c := startCore(t)
	// Someone appended x-user-id: 1 to the outgoing metadata before the
	// interceptor ran. The interceptor must replace it with the real identity.
	ctx := metadata.AppendToOutgoingContext(as(7, ""), "x-user-id", "1")
	if got := check(t, c, ctx, &gen.Request{}); got.user != 7 {
		t.Errorf("got user %d want 7", got.user)
	}
}

func TestIdentity_ReleasedBetweenCalls(t *testing.T) {
	c := startCore(t)
	// With only two stream workers, goroutines are reused. A logged-in call
	// followed by anonymous ones must not leak the earlier user.
	check(t, c, as(5, "fr"), &gen.Request{Query: "x=secret"})
	for range 10 {
		if got := check(t, c, context.Background(), &gen.Request{}); got.user != 0 || got.lang != "" || got.query != "" {
			t.Fatalf("anonymous call inherited state: %+v", got)
		}
	}
}

func TestIdentity_ConcurrentCallsIsolated(t *testing.T) {
	c := startCore(t)
	var wg sync.WaitGroup
	for i := uint32(1); i <= 50; i++ {
		wg.Add(1)
		go func(user uint32) {
			defer wg.Done()
			n := strconv.FormatUint(uint64(user), 10)
			got := check(t, c, as(user, "l"+n), &gen.Request{Query: "x=" + n})
			want := seen{user: user, lang: "l" + n, inUser: user, query: n, ctxUser: user}
			if got != want {
				t.Errorf("user %d: got %+v", user, got)
			}
		}(i)
	}
	wg.Wait()
}

func TestIdentity_StreamingRPC(t *testing.T) {
	c := startCore(t)
	stream, err := c.Upload(as(9, ""))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if err := stream.Send(&gen.UploadPayload{Chunk: []byte("x")}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	res, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv: %v", err)
	}
	if res.Message != "9" {
		t.Errorf("stream handler saw user %q want 9", res.Message)
	}
}
