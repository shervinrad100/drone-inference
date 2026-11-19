package main

import (
	"drone-target-detection/src/internal/platform"
	"drone-target-detection/src/internal/svc"
	"fmt"
	"log/slog"
	"os"
)

type StorageDestination struct{}

func (storage *StorageDestination) Write(p []byte) (n int, err error) {
	// TODO think about API design 1. primitive 2. Low level 3. High level with nested calls from prim -> HL
	// TODO handle the building of video and saving it to file
	fmt.Printf("%d bytes written to storage\n", len(p))
	return len(p), nil
}

func main() {
	// TODO start server and accept TCP connections
	// read stream
	logger := platform.NewLogger()

	serviceName, err := svc.ServiceName()
	if err != nil {
		logger.Error("this service needs a name", "err", err)
		os.Exit(1)
	}

	logger = logger.With("service.name", serviceName)
	slog.SetDefault(logger)
}
