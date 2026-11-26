// Command loadtest sends one HTTP request to every server and checks the reply.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

var request = []byte("GET / HTTP/1.0\r\n\r\n")

func main() {
	prefix := flag.String("prefix", "2001:db8:1234:5678::", "address of server 0")
	total := flag.Uint64("n", 15_000_000, "number of servers to request, starting at server 0")
	workers := flag.Int("c", 256, "concurrent connections")
	flag.Parse()

	addr, err := netip.ParseAddr(*prefix)
	if err != nil {
		log.Fatalf("prefix: %v", err)
	}
	base := addr.As16()

	var next, ok, failed atomic.Uint64
	var wg sync.WaitGroup
	start := time.Now()

	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				server := next.Add(1) - 1
				if server >= *total {
					return
				}
				if err := check(base, server); err != nil {
					if failed.Add(1) <= 10 {
						log.Printf("server %d: %v", server, err)
					}
					continue
				}
				ok.Add(1)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	const interval = 5 * time.Second
	tick := time.NewTicker(interval)
	var last uint64
	for running := true; running; {
		select {
		case <-done:
			running = false
		case <-tick.C:
			n := ok.Load()
			fmt.Printf("%9d ok %6d failed %7d req/s\n", n, failed.Load(), (n-last)/uint64(interval.Seconds()))
			last = n
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("\ndone: %d ok, %d failed in %s (%.0f req/s)\n",
		ok.Load(), failed.Load(), elapsed.Round(time.Millisecond), float64(ok.Load())/elapsed.Seconds())
}

// check sends one request to the given server and verifies that the reply
// came from that server.
func check(base [16]byte, server uint64) error {
	ip := base
	binary.BigEndian.PutUint64(ip[8:], server)
	target := netip.AddrPortFrom(netip.AddrFrom16(ip), 80)

	conn, err := net.DialTimeout("tcp6", target.String(), 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := conn.Write(request); err != nil {
		return err
	}
	reply, err := io.ReadAll(conn)
	if err != nil {
		return err
	}
	want := fmt.Appendf(nil, "\r\n\r\nserver %d, request #", server)
	if !bytes.Contains(reply, want) {
		return fmt.Errorf("unexpected reply %q", reply)
	}
	return nil
}
