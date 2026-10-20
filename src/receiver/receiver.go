package main

import (
	"drone-target-detection/src/mlmodels"
	"drone-target-detection/src/storage"
	"log/slog"
	"os"

	"flag"
	"fmt"
	"io"
	"net"
)

// TODO add logging

const (
	MAX_CONNECTIONS   = 1
	WRITE_BUFFER_SIZE = 1024
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Parse port info
	portArg := flag.String("port", "8080", "Port number to bind TCP socket")
	flag.Parse()
	address := fmt.Sprintf(":%s", *portArg)

	// Bind to TCP port
	listener, err := net.Listen("tcp", address)
	if err != nil {
		logger.Error("Failed to start server", "server.address", address, "err", err)
		return
	}
	defer listener.Close()
	logger.Info("Server up. Listening...", "server.address", address, "network.transport", "tcp")

	// TODO graceful shutdown of the server

	semaphore := make(chan struct{}, MAX_CONNECTIONS)
	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Error("Failed to accept connection.", "server.address", address, "err", err)
		}

		semaphore <- struct{}{}
		logger.Info("[+] New connection.", "client.address", conn.RemoteAddr().String(), "network.connections.active", len(semaphore))
		go handleConnection(conn, semaphore)

	}

}

func handleConnection(streamConn net.Conn, semaphore chan struct{}) {
	log := slog.With("client.address", streamConn.RemoteAddr().String())
	defer func() {
		<-semaphore
		streamConn.Close()
	}()

	// declare sources to write to
	videoStoreDest := &storage.StorageDestination{} // TODO replacce placeholder
	inferenceModel := &mlmodels.MachineLearningModel{}
	teeWriter := io.MultiWriter(videoStoreDest, inferenceModel)
	// stream frames to destination

	_, err := io.Copy(teeWriter, streamConn)
	if err != nil && err != io.EOF {
		log.Error("Failed to copy from video stream", "err", err)
	}

	// TODO frames.dropped, frames.delayed, delay_ms.max avgFps
}
