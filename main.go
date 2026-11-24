package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net/netip"

	"golang.org/x/sys/unix"
)

const (
	ipv6HeaderLen   = 40
	tcpMinHeaderLen = 20
	protoTCP        = 6
)

func main() {
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

	buff := make([]byte, 2000)
	for {
		n, err := unix.Read(fd, buff)
		if err != nil {
			log.Fatalf("read: %v", err)
		}
		describe(buff[:n])
	}
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
