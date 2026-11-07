package main

import (
	"drone-target-detection/src/internal/platform"
	"drone-target-detection/src/internal/video"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"time"
)

const (
	FAILURE_RATE_PCT        = 20
	VIDEO_FRAMES_PER_SECOND = 60
	FRAME_BUFFER_SIZE       = 1024
	MAX_DELAY_MILLISECOND   = 3600
	HEADER_SIZE             = 4 + 8 + 8 // frame id + timestamp + payload length
)

func main() {

	logger := platform.NewLogger()
	slog.SetDefault(logger)

	// parse soc info to bind to
	ipArg := flag.String("ip", "", "IP Address to send video stream")
	portArg := flag.String("port", "", "Port number for the data stream")
	flag.Parse()
	address := net.JoinHostPort(*ipArg, *portArg)

	// connect to TCP soc
	conn, err := net.Dial("tcp", address)
	if err != nil {
		logger.Error("Failed to connect to server", "network.transport", "tcp", "err", err)
		return
	}
	logger = logger.With("remote.address", conn.RemoteAddr().String(), "local.address", conn.LocalAddr().String())
	logger.Info("Connected to server.", "network.transport", "tcp")

	defer func() {
		logger.Info("closing connection")
		conn.Close()
	}()

	// start recording
	frameChan := make(chan video.Frame, 30)
	go cameraCapture(frameChan)

	// send bytes over network with possible failures
	if err := sendVideoBytes(frameChan, conn); err != nil {
		logger.Error("Failed to send bytes.", "err", err)
	}
}

// cameraCapture produces random frames at VIDEO_FRAMES_PER_SECOND.
// If the channel is full the frame is dropped. The frame ID
// still increments, so the gap is visible to whoever reads the channel.
func cameraCapture(ch chan<- video.Frame) {
	ticker := time.NewTicker(time.Second / VIDEO_FRAMES_PER_SECOND)
	slog.Info("camera starting...")

	defer func() {
		slog.Info("stopping camera")
		ticker.Stop()
	}()

	var frameID uint32
	for range ticker.C {
		frameID++
		payload := make([]byte, FRAME_BUFFER_SIZE)
		for i := range payload {
			payload[i] = byte(rand.UintN(256))
		}
		frame := video.Frame{ID: frameID, Timestamp: time.Now(), Payload: payload}

		select {
		case ch <- frame:
		default: // channel full: drop the frame
		}
	}

	// TODO shutdown with ctx
}

func sendVideoBytes(frameChan <-chan video.Frame, writer io.Writer) error {
	for frame := range frameChan {
		// with a chance of failure
		if rand.IntN(100) < FAILURE_RATE_PCT {
			switch rand.IntN(2) {
			case 0: // random delays in delivery
				time.Sleep(rand.N(MAX_DELAY_MILLISECOND * time.Millisecond))
			case 1: // random packet drop
				continue
			}
		}

		// send over network
		if _, err := writer.Write(video.EncodeFrame(frame)); err != nil {
			return fmt.Errorf("failed to write frame %d: %w", frame.ID, err)
		}
	}
	return nil
}
