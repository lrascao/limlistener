package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/lrascao/limlistener"
)

const (
	BYTE = 1 << (10 * iota)
	KILOBYTE
	MEGABYTE
)

func main() {
	l, err := net.Listen("tcp", ":7000")
	if err != nil {
		log.Fatal(err)
	}
	ll := limlistener.NewWithListener(l)
	defer ll.Close()

	// cap bandwidth globally at 20 MB/sec and per-connection at 5 MB/sec
	globalLimit := 20 * MEGABYTE
	connLimit := 5 * MEGABYTE
	ll.SetLimits(globalLimit, connLimit)

	fmt.Printf("Listening on port 7000\n")
	for {
		// Wait for a connection.
		conn, err := ll.Accept()
		if err != nil {
			log.Fatal(err)
		}

		// each connection will have it's bandwidth doubled until it hits
		// the global limit, from then on the global limit is increased by 10%
		// on every connection
		if connLimit >= globalLimit {
			globalLimit += int(float64(globalLimit) * 0.10)
		} else {
			connLimit *= 2
		}
		fmt.Printf("connection accepted, throttling at %d MB/sec, global: %d MB/sec\n", connLimit/MEGABYTE, globalLimit/MEGABYTE)
		ll.SetLimits(globalLimit, connLimit)

		go func(conn net.Conn) {
			// Shut down the connection.
			defer func() {
				ll.CloseConnection(conn)
			}()

			f, err := os.Open("data-file")
			if err != nil {
				log.Fatal(err)
			}
			// Copy all incoming data and time it
			start := time.Now()
			n, err := io.Copy(conn, f)
			elapsed := time.Since(start)
			if err != nil {
				log.Fatal(err)
			}
			bytesPerSec := (n * 1000) / elapsed.Milliseconds()
			fmt.Printf("%d bytes copied in %dms (%d MB/sec)\n", n, elapsed.Milliseconds(), bytesPerSec/MEGABYTE)
		}(conn)
	}
}
