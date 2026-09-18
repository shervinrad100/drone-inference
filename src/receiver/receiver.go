package main

import (
	"drone-target-detection/src/ml_models"
	"drone-target-detection/src/storage"

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
	// Parse port info
	portArg := flag.String("port", "8080", "Port number to bind TCP socket")
	address := fmt.Sprintf(":%s", *portArg)
	flag.Parse()

	// Bind to TCP port
	listener, err := net.Listen("tcp", address)
	fmt.Printf("LOG: listening on %s...\n", address)
	defer listener.Close()
	if err != nil {
		fmt.Printf("ERROR: failed to listen on %v: %w/n", address, err)
		return
	}

	// TODO graceful shutdown of the server

	semaphore := make(chan struct{}, MAX_CONNECTIONS)
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("ERROR: failed to accept connection on %v: %w", address, err)
		}

		semaphore <- struct{}{}
		fmt.Printf("[+] New connection from %s (%d active)\n", conn.RemoteAddr(), len(semaphore))
		go handleConnection(conn, semaphore)

	}

}

func handleConnection(streamConn net.Conn, semaphore chan struct{}) {
	defer func() {
		<-semaphore
		streamConn.Close()
	}()

	// declare sources to write to
	videoStoreDest := &storage.StorageDestination{} // TODO replacce placeholder
	inferenceModel := &ml_models.MachineLearningModel{}
	teeWriter := io.MultiWriter(videoStoreDest, inferenceModel)
	// stream frames to destination

	_, err := io.Copy(teeWriter, streamConn)
	if err != nil && err != io.EOF {
		// TODO logging
		fmt.Printf("ERROR: failed to write from video stream: %w", err)
	}

}
