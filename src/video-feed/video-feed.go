package main

import (
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"time"
)

const (
	FAILURE_RATE_PCT        = 20
	VIDEO_FRAMES_PER_SECOND = 60
	WRITE_BUFFER_SIZE       = 1024
	MAX_DELAY_MILLISECOND   = 3500
)

func main() {
	// parse soc info to bind to
	ipArg := flag.String("ip", "", "IP Address to send video stream")
	portArg := flag.String("port", "", "Port number for the data stream")
	flag.Parse()
	address := fmt.Sprintf("%s:%s", *ipArg, *portArg)

	// connect to TCP soc
	conn, err := net.Dial("tcp", address)
	defer conn.Close()
	if err != nil {
		fmt.Printf("ERROR: Failed to connect to soc: %w\n", err)
		return
	}

	// send random bytes mocking a video feed
	if err := sendVideoBytes(conn); err != nil {
		fmt.Printf("ERROR: %w\n", err)
	}
}

func sendVideoBytes(writer io.Writer) error {
	// send random bytes 60 frames per sec with a chance of failure

	interval := time.Second / VIDEO_FRAMES_PER_SECOND
	ticker := time.NewTicker(interval) // start a go routine sends the current time to channel at interval
	defer ticker.Stop()
	frameBuffer := make([]byte, WRITE_BUFFER_SIZE)

	for range ticker.C {
		failure := rand.IntN(100) < FAILURE_RATE_PCT
		if failure {
			switch rand.IntN(2) {
			case 0: // random delays in delivery
				time.Sleep(rand.N(MAX_DELAY_MILLISECOND * time.Millisecond))
			case 1: // random packet drop
				continue

			}
		}

		for i := range frameBuffer {
			frameBuffer[i] = byte(rand.UintN(256))
		}
		if _, err := writer.Write(frameBuffer); err != nil {
			return fmt.Errorf("ERROR: failed to write bytes: %w\n", err)
		}

	}
	return nil

}
