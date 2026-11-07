package main

import (
	"drone-target-detection/src/services/mlmodels"
	"drone-target-detection/src/services/storage"
	"log/slog"
	"os"

	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// TODO add logging

const (
	MAX_CONNECTIONS = 1
	// frame layout must match encodeFrame in video-feed.go:
	// id (4 bytes) | timestamp ns (8 bytes) | payload length (8 bytes) | payload
	HEADER_SIZE       = 4 + 8 + 8
	MAX_PAYLOAD_BYTES = 1 << 20 // reject corrupt headers before allocating

	STATS_INTERVAL          = time.Second
	DELAYED_FRAME_THRESHOLD = time.Second / 60 // older than one frame interval at 60fps
)

func main() {
	var logLevel slog.Level
	switch strings.ToUpper(os.Getenv("LOG_LEVEL")) {
	case "DEBUG":
		logLevel = slog.LevelDebug
	case "INFO":
		logLevel = slog.LevelInfo
	case "WARN":
		logLevel = slog.LevelWarn
	case "ERROR":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{
		Level: logLevel,
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))
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

		// refuse when at capacity instead of leaving the client hanging
		select {
		case semaphore <- struct{}{}:
		default:
			logger.Warn("Connection refused: at capacity.", "client.address", conn.RemoteAddr().String(), "network.connections.max", MAX_CONNECTIONS)
			conn.Close()
			continue
		}
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

	// stream health measured from what arrives (see streamStats below)
	stats := streamStats{windowStart: time.Now()}
	defer stats.report(log) // log the final partial window on exit

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

		stats.observe(frame)
		if time.Since(stats.windowStart) >= STATS_INTERVAL {
			stats.report(log)
		}

		if _, err := teeWriter.Write(frame.Payload); err != nil {
			log.Error("Failed to forward frame", "frame.id", frame.ID, "err", err)
			return
		}
	}
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

// streamStats measures stream health from what arrives: gaps in frame IDs
// (drops anywhere upstream) and frame age (delay). Reset every window.
type streamStats struct {
	windowStart time.Time
	lastID      uint32 // carried across windows
	received    uint64
	dropped     uint64
	delayed     uint64
	delaySum    time.Duration
	delayMax    time.Duration
}

func (s *streamStats) observe(f Frame) {
	s.received++
	if f.ID > s.lastID {
		s.dropped += uint64(f.ID - s.lastID - 1)
	}
	s.lastID = f.ID

	// only meaningful when sender and receiver clocks agree
	delay := time.Since(f.Timestamp)
	s.delaySum += delay
	s.delayMax = max(s.delayMax, delay)
	if delay > DELAYED_FRAME_THRESHOLD {
		s.delayed++
	}
}

// report logs the current window and starts a new one.
func (s *streamStats) report(log *slog.Logger) {
	elapsed := time.Since(s.windowStart)
	var delayAvg time.Duration
	if s.received > 0 {
		delayAvg = s.delaySum / time.Duration(s.received)
	}
	log.Info("Stream stats",
		"frames.received", s.received,
		"frames.dropped", s.dropped,
		"frames.delayed", s.delayed,
		"delay_ms.avg", float64(delayAvg)/float64(time.Millisecond),
		"delay_ms.max", float64(s.delayMax)/float64(time.Millisecond),
		"fps.avg", float64(s.received)/elapsed.Seconds(),
	)
	*s = streamStats{windowStart: time.Now(), lastID: s.lastID}
}
