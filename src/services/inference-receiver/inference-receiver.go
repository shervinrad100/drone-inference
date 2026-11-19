package main

import (
	"drone-target-detection/src/internal/platform"
	"drone-target-detection/src/internal/svc"
	"drone-target-detection/src/internal/video"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
)

const (
	MAX_CONNECTIONS = 1
)

type MachineLearningModel struct{}

func (inferenceModel *MachineLearningModel) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func main() {

	logger := platform.NewLogger()

	serviceName, err := svc.ServiceName()
	if err != nil {
		logger.Error("this service needs a name", "err", err)
		os.Exit(1)
	}

	logger = logger.With("service.name", serviceName)
	slog.SetDefault(logger)

	// receive streams
	portArg := flag.String("port", "8080", "Port number to bind TCP socket")
	flag.Parse()
	address := fmt.Sprintf(":%s", *portArg)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		logger.Error("Failed to start server", "local.address", address, "err", err)
		return
	}
	logger = logger.With("local.address", listener.Addr().String())
	slog.SetDefault(logger)
	logger.Info("Server up. Listening...", "network.transport", "tcp")

	defer func() {
		logger.Info("closing listener")
		listener.Close()
	}()

	// TODO graceful shutdown of the server

	// Listen for connections
	semaphore := make(chan struct{}, MAX_CONNECTIONS)
	for {
		conn, err := listener.Accept()
		if err != nil {
			// a closed listener fails every Accept, so retrying would spin forever
			if errors.Is(err, net.ErrClosed) {
				logger.Info("Listener closed, no longer accepting connections")
				return
			}
			logger.Error("Failed to accept connection.", "err", err)
			continue
		}

		// refuse when at capacity instead of leaving the client hanging
		select {
		case semaphore <- struct{}{}:
		default:
			logger.Warn("Connection refused: at capacity.", "remote.address", conn.RemoteAddr().String(), "network.connections.max", MAX_CONNECTIONS)
			conn.Close()
			continue
		}
		logger.Info("[+] New connection.", "remote.address", conn.RemoteAddr().String(), "network.connections.active", len(semaphore))

		inferenceModel := &MachineLearningModel{}
		go handleVideoFeed(inferenceModel, conn, semaphore)

	}

}

func handleVideoFeed(inferenceModel *MachineLearningModel, conn net.Conn, semaphore chan struct{}) {
	logger := slog.With("remote.address", conn.RemoteAddr().String())
	defer func() {
		<-semaphore
		logger.Info("Closing connection")
		conn.Close()
	}()

	for {
		frame, err := video.ReadFrame(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				logger.Error("failed to read frame from connection", "err", err)
			} else if errors.Is(err, io.EOF) {
				logger.Info("Stream closed by sender")
				return
			}
		}
		inferenceModel.Write(frame.Payload)
	}
}
