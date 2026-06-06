package interceptor

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Logging(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	log.Printf(">>> %s", info.FullMethod)
	start := time.Now()
	resp, err := handler(ctx, req)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("<<< %s ERROR (%s): %v", info.FullMethod, elapsed, err)
	} else {
		log.Printf("<<< %s OK (%s)", info.FullMethod, elapsed)
	}
	return resp, err
}

func UnknownService(_ any, stream grpc.ServerStream) error {
	fullMethod, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		fullMethod = "<unknown>"
	}
	log.Printf(">>> %s", fullMethod)
	err := status.Errorf(codes.Unimplemented, "unknown service or method %s", fullMethod)
	log.Printf("<<< %s ERROR: %v", fullMethod, err)
	return err
}
