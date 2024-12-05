package lib

import (
	"context"

	"github.com/dimk00z/grpc-filetransfer/pkg/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var l *logger.Logger

func init() {
	// Assuming your logger has a New function that initializes a logger
	l = logger.New("debug")
}

func LogError(err error) error {
	if err != nil {
		l.Error(err)
	}
	return err
}

func ContextError(ctx context.Context) error {
	switch ctx.Err() {
	case context.Canceled:
		return LogError(status.Error(codes.Canceled, "request is canceled"))
	case context.DeadlineExceeded:
		return LogError(status.Error(codes.DeadlineExceeded, "deadline is exceeded"))
	default:
		return nil
	}
}
