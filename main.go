package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net/netip"

	"golang.org/x/sys/unix"
)

const (
	ipv6HeaderLen   = 40
	tcpMinHeaderLen = 20
	protoTCP        = 6
	flagSYN         = 0x02
	flagACK         = 0x10
	flagRST         = 0x04
	httpPort        = 80
	numServers      = 15_000_000
	flagFIN         = 0x01
	flagPSH         = 0x08
	serverISN       = 1000
)

var (
	// counters holds one request counter per server, indexed by server number.
	counters = make([]uint32, numServers)

	// Scratch space for building a response. Reused for every request.
	bodyBuf = make([]byte, 0, 64)
	respBuf = make([]byte, 0, 256)
)

func main() {
	verbose := flag.Bool("v", false, "print every packet")
	flag.Parse()

	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		log.Fatalf("open tun: %v", err)
	}
	defer unix.Close(fd)

	ifr, err := unix.NewIfreq("tun0")
	if err != nil {
		log.Fatalf("ifreq: %v", err)
	}
	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI)

	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		log.Fatalf("TUNSETIFF: %v", err)
	}
	fmt.Println("attached to tun0, fd:", fd)
	fmt.Printf("%d servers, counter table %d MB\n", numServers, numServers*4/1_000_000)

	buff := make([]byte, 2000)
	out := make([]byte, 2000)
	for {
		n, err := unix.Read(fd, buff)
		if err != nil {
			log.Fatalf("read: %v", err)
		}
		if *verbose {
			describe(buff[:n])
		}

		if reply := handle(buff[:n], out); reply != nil {
			if _, err := unix.Write(fd, reply); err != nil {
				log.Fatalf("write: %v", err)
			}
		}
	}
}

// handle returns the packet to send in response to pkt, or nil for none.
// The reply is built inside out, so it is only valid until the next call.
func handle(pkt, out []byte) []byte {
	if len(pkt) < ipv6HeaderLen+tcpMinHeaderLen || pkt[0]>>4 != 6 || pkt[6] != protoTCP {
		return nil
	}
	tcp := pkt[ipv6HeaderLen:]
	headerLen := int(tcp[12]>>4) * 4
	if headerLen < tcpMinHeaderLen || headerLen > len(tcp) {
		return nil
	}
	flags := tcp[13]
	dstPort := binary.BigEndian.Uint16(tcp[2:4])
	seq := binary.BigEndian.Uint32(tcp[4:8])
	ack := binary.BigEndian.Uint32(tcp[8:12])
	data := tcp[headerLen:]
	server, isServer := serverIndex(pkt[24:40])

	// How far this segment advances the client's sequence numbers:
	// one per data byte, plus one each for SYN and FIN.
	segLen := uint32(len(data))
	if flags&flagSYN != 0 {
		segLen++
	}
	if flags&flagFIN != 0 {
		segLen++
	}

	switch {
	case flags&flagRST != 0:
		// The client aborted. Never answer a reset.
		return nil
	case !isServer || dstPort != httpPort:
		// Not one of our servers, or not the port they listen on: refuse.
		return buildReply(pkt, out, ack, seq+segLen, flagRST|flagACK, nil)
	case flags == flagSYN:
		// Open: SYN-ACK.
		return buildReply(pkt, out, serverISN, seq+segLen, flagSYN|flagACK, nil)
	case flags&flagACK == 0 || flags&flagSYN != 0:
		// Neither a valid opening nor part of a connection: abort it.
		return buildReply(pkt, out, ack, seq+segLen, flagRST|flagACK, nil)
	case len(data) > 0:
		// Request: acknowledge it, send the response, close our side.
		counters[server]++
		return buildReply(pkt, out, ack, seq+segLen, flagFIN|flagPSH|flagACK, response(server, counters[server]))
	case flags&flagFIN != 0:
		// Client closed its side: acknowledge.
		return buildReply(pkt, out, ack, seq+segLen, flagACK, nil)
	}
	// A bare ACK needs no answer.
	return nil
}

// serverIndex maps a destination address to a server number.
// The server number is the low 64 bits of the address.
func serverIndex(dst []byte) (uint64, bool) {
	n := binary.BigEndian.Uint64(dst[8:16])
	return n, n < numServers
}

// response builds the HTTP reply for one request to one server.
// The result lives in respBuf, so it is only valid until the next call.
func response(server uint64, count uint32) []byte {
	body := fmt.Appendf(bodyBuf[:0], "server %d, request #%d\n", server, count)
	return fmt.Appendf(respBuf[:0], "HTTP/1.0 200 OK\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
}

// buildReply writes a TCP/IPv6 packet into out that answers pkt:
// addresses and ports swapped, with the given sequence numbers, flags and data.
func buildReply(pkt, out []byte, seq, ack uint32, flags byte, data []byte) []byte {
	tcpLen := tcpMinHeaderLen + len(data)
	reply := out[:ipv6HeaderLen+tcpLen]
	clear(reply[:ipv6HeaderLen+tcpMinHeaderLen])

	// IPv6 header
	reply[0] = 0x60
	binary.BigEndian.PutUint16(reply[4:6], uint16(tcpLen))
	reply[6] = protoTCP
	reply[7] = 64
	copy(reply[8:24], pkt[24:40]) // our source = their destination
	copy(reply[24:40], pkt[8:24]) // our destination = their source

	// TCP header
	in := pkt[ipv6HeaderLen:]
	rt := reply[ipv6HeaderLen:]
	copy(rt[0:2], in[2:4]) // our source port = their destination port
	copy(rt[2:4], in[0:2]) // our destination port = their source port
	binary.BigEndian.PutUint32(rt[4:8], seq)
	binary.BigEndian.PutUint32(rt[8:12], ack)
	rt[12] = 5 << 4
	rt[13] = flags
	binary.BigEndian.PutUint16(rt[14:16], 65535)
	copy(rt[tcpMinHeaderLen:], data)
	binary.BigEndian.PutUint16(rt[16:18], checksum(reply[8:24], reply[24:40], rt))

	return reply
}

func describe(pkt []byte) {
	if len(pkt) < ipv6HeaderLen {
		fmt.Printf("too short: %d bytes\n", len(pkt))
		return
	}
	if version := pkt[0] >> 4; version != 6 {
		fmt.Printf("tis aint IPv6. version: %d\n", version)
		return
	}
	payloadLen := binary.BigEndian.Uint16(pkt[4:6])
	protocol := pkt[6]
	hopLimit := pkt[7]
	src := netip.AddrFrom16([16]byte(pkt[8:24]))
	dst := netip.AddrFrom16([16]byte(pkt[24:40]))

	fmt.Printf("%s -> %s proto=%d hop=%d payload=%d\n", src, dst, protocol, hopLimit, payloadLen)

	if protocol != protoTCP {
		fmt.Printf("protocol not tcp mate\n")
		return
	}

	tcp := pkt[ipv6HeaderLen:]
	if len(tcp) < tcpMinHeaderLen {
		fmt.Printf("tcp header too short: %d bytes\n", len(tcp))
		return
	}

	srcPort := binary.BigEndian.Uint16(tcp[0:2])
	dstPort := binary.BigEndian.Uint16(tcp[2:4])
	seq := binary.BigEndian.Uint32(tcp[4:8])
	ack := binary.BigEndian.Uint32(tcp[8:12])
	headerLen := int(tcp[12]>>4) * 4
	flags := tcp[13]
	window := binary.BigEndian.Uint16(tcp[14:16])

	fmt.Printf("  tcp %d -> %d seq=%d ack=%d hdr=%d flags=%#02x win=%d\n",
		srcPort, dstPort, seq, ack, headerLen, flags, window)

	fmt.Printf("  checksum verify=%#04x\n", checksum(pkt[8:24], pkt[24:40], tcp))

	if headerLen >= tcpMinHeaderLen && headerLen < len(tcp) {
		fmt.Printf("  data: %q\n", tcp[headerLen:])
	}
}

func checksum(src, dst, segment []byte) uint16 {
	var sum uint32
	add := func(b []byte) {
		for len(b) >= 2 {
			sum += uint32((binary.BigEndian.Uint16(b)))
			b = b[2:]
		}
		if len(b) == 1 {
			sum += uint32(b[0]) << 8
		}
	}
	add(src)
	add(dst)
	sum += uint32((len(segment)))
	sum += protoTCP
	add(segment)

	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}
