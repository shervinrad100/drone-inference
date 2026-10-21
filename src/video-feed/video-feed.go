package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"strings"
	"time"
)

const (
	FAILURE_RATE_PCT        = 20
	VIDEO_FRAMES_PER_SECOND = 60
	FRAME_BUFFER_SIZE       = 1024
	MAX_DELAY_MILLISECOND   = 20
	HEADER_SIZE             = 4 + 8 + 8 // frame id + timestamp + payload length
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

	// parse soc info to bind to
	ipArg := flag.String("ip", "", "IP Address to send video stream")
	portArg := flag.String("port", "", "Port number for the data stream")
	flag.Parse()
	address := fmt.Sprintf("%s:%s", *ipArg, *portArg)
	logger = logger.With("server.address", address)

	// connect to TCP soc
	conn, err := net.Dial("tcp", address)
	if err != nil {
		logger.Error("Failed to connect to server", "network.transport", "tcp", "err", err)
		return
	}
	defer conn.Close()
	logger = logger.With("client.address", conn.LocalAddr().String())
	logger.Info("Connected to server.", "network.transport", "tcp")

	// start recording
	frameChan := make(chan Frame, 30)
	go cameraCapture(frameChan)
	// send bytes over network with possible failures
	if err := sendVideoBytes(logger, frameChan, conn); err != nil {
		logger.Error("Failed to send bytes.", "err", err)
	}
}

type Frame struct {
	ID        uint32
	Timestamp time.Time
	Payload   []byte
}

// cameraCapture produces random frames at VIDEO_FRAMES_PER_SECOND.
// If the channel is full the frame is dropped silently. The frame ID
// still increments, so the gap is visible to whoever reads the channel.
func cameraCapture(ch chan<- Frame) {
	ticker := time.NewTicker(time.Second / VIDEO_FRAMES_PER_SECOND)
	defer ticker.Stop()

	var frameID uint32
	for range ticker.C {
		frameID++
		payload := make([]byte, FRAME_BUFFER_SIZE)
		for i := range payload {
			payload[i] = byte(rand.UintN(256))
		}
		frame := Frame{ID: frameID, Timestamp: time.Now(), Payload: payload}

		select {
		case ch <- frame:
		default: // channel full: drop the frame
		}
	}
	// TODO shutdown with ctx
}

// encodeFrame packs a frame for the wire:
// id (4 bytes) | timestamp ns (8 bytes) | payload length (8 bytes) | payload
func encodeFrame(f Frame) []byte {
	buf := make([]byte, 0, HEADER_SIZE+len(f.Payload))
	buf = binary.BigEndian.AppendUint32(buf, f.ID)
	buf = binary.BigEndian.AppendUint64(buf, uint64(f.Timestamp.UnixNano()))
	buf = binary.BigEndian.AppendUint64(buf, uint64(len(f.Payload)))
	return append(buf, f.Payload...)
}

func sendVideoBytes(log *slog.Logger, frameChan <-chan Frame, writer io.Writer) error {
	// gaps in frame IDs mean cameraCapture dropped frames (buffer full).
	// counted here and logged once a second rather than per frame.
	var lastID, dropped uint32
	lastReport := time.Now()

	for frame := range frameChan {
		dropped += frame.ID - lastID - 1
		lastID = frame.ID
		if time.Since(lastReport) >= time.Second {
			if dropped > 0 {
				log.Warn("Frames dropped", "frames.dropped", dropped, "reason", "buffer_full")
			}
			dropped = 0
			lastReport = time.Now()
		}

		//  with a chance of failure
		failure := rand.IntN(100) < FAILURE_RATE_PCT
		if failure {
			switch rand.IntN(2) {
			case 0: // random delays in delivery
				time.Sleep(rand.N(MAX_DELAY_MILLISECOND * time.Millisecond))
			case 1: // random packet drop
				continue
			}
		}

		// send over network
		if _, err := writer.Write(encodeFrame(frame)); err != nil {
			return fmt.Errorf("failed to write frame %d: %w", frame.ID, err)
		}
	}
	return nil
}
