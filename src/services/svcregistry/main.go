package main

import (
	"context"
	"drone-target-detection/src/internal/platform"
	"drone-target-detection/src/internal/svc"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// TODO let's make service registration a command line tool that creates services and registers them automatically

// TODO migrate to grpc
// TODO watch so that if someone deregisters it can be pushed to all services.
// TODO only push to services that need to know -> create service lineage

// client caches latest registry

func writeJson(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	resp, err := json.Marshal(body)
	if err != nil {
		slog.Error("failed to marshal response", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(code)
	if _, err := w.Write(resp); err != nil {
		slog.Debug("failed to write response", "err", err)
	}
}

type server struct {
	registry Registry
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	// endpoints
	mux.HandleFunc("GET /services", s.getServices)
	mux.HandleFunc("GET /services/{serviceId}", s.getServiceById)
	mux.HandleFunc("POST /services/{serviceId}/instances", s.registerInstance)
	mux.HandleFunc("DELETE /services/{serviceId}/instances/{instanceId}", s.deregisterInstance)
	mux.HandleFunc("PUT /services/{serviceId}/instances/{instanceId}/lease", s.renewLease)

	return mux
}

func (s *server) registerInstance(w http.ResponseWriter, req *http.Request) {
	// get the parameters from path
	serviceId := req.PathValue("serviceId")

	// deal with the body
	// make sure POST body is limited
	req.Body = http.MaxBytesReader(w, req.Body, 4<<10)
	// unmarshal request
	var regInstanceReq svc.RegisterInstanceRequest
	if err := json.UnmarshalRead(req.Body, &regInstanceReq, json.RejectUnknownMembers(true)); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "bad request", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// execute logic
	instance, err := s.registry.register(serviceId, regInstanceReq.Address)
	if err != nil {
		if errors.Is(err, ErrInvalidAddress) || errors.Is(err, ErrInvalidServiceName) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// respond
	w.Header().Set("Location", fmt.Sprintf("/services/%s/instances/%s", serviceId, instance.ID))
	writeJson(w, http.StatusCreated, instance)
}

func (s *server) getServiceById(w http.ResponseWriter, req *http.Request) {
	// TODO
	http.Error(w, "not implemented yet", http.StatusInternalServerError)
}

func (s *server) getServices(w http.ResponseWriter, req *http.Request) {
	// TODO
	http.Error(w, "not implemented yet", http.StatusInternalServerError)
}

func (s *server) deregisterInstance(w http.ResponseWriter, req *http.Request) {
	// TODO
	http.Error(w, "not implemented yet", http.StatusInternalServerError)
}

func (s *server) renewLease(w http.ResponseWriter, req *http.Request) {
	// TODO
	http.Error(w, "not implemented yet", http.StatusInternalServerError)
}

func run() error {
	// Start app
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := &server{registry: newRegistry()}
	var wg sync.WaitGroup
	wg.Go(func() { app.registry.monitor(ctx) })

	// set up http server
	// TODO specify which port ListenAndServe listens on. read from env var
	// TODO http server has no timeouts so clients can hold cons open indefinitely
	// TODO http.maxBytesReader
	srv := &http.Server{
		Addr:              ":8080", // TODO what address should this run on?
		Handler:           app.routes(),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "local.address", srv.Addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down", "reason", context.Cause(ctx))
	case err := <-serveErr:
		stop() // stop the app's context because http server has stopped
		wg.Wait()
		return err
	}

	// finish the inflight requests with a temp context
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := srv.Shutdown(shutdownCtx)

	wg.Wait()
	return err

}

func main() {
	logger := platform.NewLogger()
	// logger = logger.With("", ) // TODO name of this service and address etc
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}

}
