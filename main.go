package main

import (
	"fmt"
	"log"

	"golang.org/x/sys/unix"
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
		fmt.Printf("%d bytes: %x\n", n, buff[:n])
	}
}
