package main

import (
	"context"
	"drone-target-detection/src/internal/platform"
	"log/slog"
	"sync"
)

// so we need a server with the following endpoints
// 		POST svc/register svc // TODO let's make this command line tool that creates services and registers them automatically and tests them as bedrock
// 		PUT svc/{name}/ready ready and up acts as the heartbeat
// 		GET svc/{name} sends information about that svc
// 		GET svc dump all services
// 		TODO DELETE svc/register/{name} deregisters the service

// TODO migrate to grpc
// TODO watch so that if someone deregisters it can be pushed to all services.
// TODO only push to services that need to know -> create service lineage

// client caches latest registry

func main() {
	logger := platform.NewLogger()
	// logger = logger.With("", ) // TODO name of this service and address etc
	slog.SetDefault(logger)

	// Set up registry
	registry := newRegistry()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	var wg sync.WaitGroup
	wg.Go(func() { registry.monitor(ctx) })

	// start HTTP server
	// TODO

	<-ctx.Done()
	wg.Wait()
}
