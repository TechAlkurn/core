package config

import (
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"

	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	grpcOnce sync.Once
	GRPC     *grpc.ClientConn
)

func InitGrpcClient() {
	grpcOnce.Do(func() {
		target := os.Getenv("GRPC_TARGETS")
		if target == "" {
			port := os.Getenv("GRPC_PORT")
			if port == "" {
				port = "50050"
			}
			target = "localhost:" + port
			Log.Warnf("GRPC_TARGETS not set, defaulting to %s", target)
		}
		conn, err := grpc.NewClient(
			target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithUnaryInterceptor(GrpcAuthClientUnary),
			grpc.WithStreamInterceptor(GrpcAuthClientStream),
			grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
			grpc.WithConnectParams(grpc.ConnectParams{
				MinConnectTimeout: 15 * time.Second,
				Backoff: backoff.Config{
					BaseDelay:  1.0 * time.Second,
					Multiplier: 1.6,
					MaxDelay:   30 * time.Second,
				},
			}),
		)
		if err != nil {
			Log.Fatalf("Failed to create gRPC client: %v", err)
		}
		GRPC = conn
	})
}
