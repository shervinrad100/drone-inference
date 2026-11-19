package main

import (
	"drone-target-detection/src/internal/platform"
	"drone-target-detection/src/internal/svc"
	"drone-target-detection/src/internal/video"
	"flag"
	"log/slog"
	"os"

	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	MAX_CONNECTIONS = 1

	STATS_INTERVAL          = time.Second
	DELAYED_FRAME_THRESHOLD = time.Second / 60 // older than one frame interval at 60fps
)

func main() {

	logger := platform.NewLogger()

	serviceName, err := svc.ServiceName()
	if err != nil {
		logger.Error("this service needs a name", "err", err)
		os.Exit(1)
	}

	logger = logger.With("service.name", serviceName)
	slog.SetDefault(logger)

	// Start new TCP server
	portArg := flag.String("port", "", "Port number to listen on")
	flag.Parse()
	address := fmt.Sprintf(":%s", *portArg)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		logger.Error("Failed to start server", "err", err)
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

		go handleVideoFeed(conn, semaphore)

	}

}

func handleVideoFeed(streamConn net.Conn, semaphore chan struct{}) {
	logger := slog.With("remote.address", streamConn.RemoteAddr().String())
	defer func() {
		<-semaphore
		logger.Info("closing connection")
		streamConn.Close()
	}()

	// declare sources to write to
	// TODO send to inference model and video store
	// TODO get resource destination from service registry
	// TODO if errors, get from registry to check, retry with another instance
	// teeWriter := io.MultiWriter(videoStoreDest, inferenceModelDest)

	// stream health measured from what arrives (see streamStats below)
	stats := streamStats{windowStart: time.Now()}
	defer stats.report(logger) // log the final partial window on exit

	// read one whole frame at a time and forward its payload,
	// so each destination gets exactly one frame per Write call
	for {
		frame, err := video.ReadFrame(streamConn)
		if err == io.EOF {
			logger.Info("Stream closed by sender")
			return
		}
		if err != nil {
			logger.Error("Failed to read frame", "err", err)
			return
		}

		stats.observe(frame)
		if time.Since(stats.windowStart) >= STATS_INTERVAL {
			stats.report(logger)
		}

		// TODO forward frame downstream
		// if _, err := teeWriter.Write(frame.Payload); err != nil {
		// 	logger.Error("Failed to forward frame", "frame.id", frame.ID, "err", err)
		// 	return
		// }
	}
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

func (s *streamStats) observe(f video.Frame) {
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
