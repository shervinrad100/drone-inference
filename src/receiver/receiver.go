package main

import (
	"drone-target-detection/src/mlmodels"
	"drone-target-detection/src/storage"
	"log/slog"
	"os"

	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"time"
)

// TODO add logging

const (
	MAX_CONNECTIONS = 1
	// frame layout must match encodeFrame in video-feed.go:
	// id (4 bytes) | timestamp ns (8 bytes) | payload length (8 bytes) | payload
	HEADER_SIZE       = 4 + 8 + 8
	MAX_PAYLOAD_BYTES = 1 << 20 // reject corrupt headers before allocating
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
			continue
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
	videoStoreDest := &storage.StorageDestination{} // TODO
	inferenceModel := &mlmodels.MachineLearningModel{}
	teeWriter := io.MultiWriter(videoStoreDest, inferenceModel)

	// read one whole frame at a time and forward its payload,
	// so each destination gets exactly one frame per Write call
	for {
		frame, err := readFrame(streamConn)
		if err == io.EOF {
			log.Info("Stream closed by sender.")
			return
		}
		if err != nil {
			log.Error("Failed to read frame", "err", err)
			return
		}
		if _, err := teeWriter.Write(frame.Payload); err != nil {
			log.Error("Failed to forward frame", "frame.id", frame.ID, "err", err)
			return
		}
	}

	// TODO frames.dropped, frames.delayed, delay_ms.max avgFps
}

type Frame struct {
	ID        uint32
	Timestamp time.Time
	Payload   []byte
}

// readFrame reads exactly one frame from r.
// Returns io.EOF only if the stream ended cleanly between frames;
// a stream that stops mid-frame returns io.ErrUnexpectedEOF.
func readFrame(r io.Reader) (Frame, error) {
	hdr := make([]byte, HEADER_SIZE)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return Frame{}, err
	}

	length := binary.BigEndian.Uint64(hdr[12:20])
	if length > MAX_PAYLOAD_BYTES {
		return Frame{}, fmt.Errorf("payload length %d exceeds max %d", length, MAX_PAYLOAD_BYTES)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF // got a header but no payload
		}
		return Frame{}, err
	}

	return Frame{
		ID:        binary.BigEndian.Uint32(hdr[0:4]),
		Timestamp: time.Unix(0, int64(binary.BigEndian.Uint64(hdr[4:12]))),
		Payload:   payload,
	}, nil
}
