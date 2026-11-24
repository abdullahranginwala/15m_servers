# 15m_servers

An attempt to run 15 million web servers on one machine.

Current count: **0**.

## Why

I read [15 million web servers on one VM](https://kapteko.com/15-million-webservers/) and
had the standard reaction: "that doesn't look so hard". This repo is where I find out
how wrong that is, in Go, one packet at a time.

The trick in the post is that you don't ask the kernel for 15 million sockets. You ask
it for zero. You take a whole subnet, point it at a TUN device, and answer the raw IP
packets yourself. Every address in the subnet becomes a "server", and a server that
nobody is talking to costs you nothing, because it doesn't exist until a packet shows up
looking for it.

## What it does today

It opens `/dev/net/tun`, attaches to `tun0`, and reads every packet the kernel hands
it. Then it tells you what it saw:

- IPv6 header: source, destination, next protocol, hop limit, payload length
- TCP header: ports, seq, ack, header length, flags, window
- TCP checksum, recomputed over the pseudo-header and segment, so a healthy packet
  prints zero

IPv4 gets turned away at the door. So does anything that isn't TCP.

It still does not reply. It has graduated from a very attentive `/dev/null` to a
very judgmental one.

## Running it

Linux only. macOS has no `/dev/net/tun`, so there it just dies on line one.

```sh
go build -o tunservers .
sudo ./tunservers
```

In a second terminal, bring the interface up and give it a /64, which is 18 quintillion
addresses to worry about. 15 million of them should be enough for now.

```sh
sudo ip link set tun0 up
sudo ip -6 addr add fd00::1/64 dev tun0
```

`fd00::1` is you. Every other address in there is a server that doesn't exist yet.
Go talk to one:

```sh
curl 'http://[fd00::2]/'
```

The first terminal prints something like:

```
attached to tun0, fd: 3
fd00::1 -> fd00::2 proto=6 hop=64 payload=40
  tcp 51234 -> 80 seq=2891374650 ack=0 hdr=40 flags=0x2 win=64800
  checksum verify=0x00
```

`flags=0x2` is a SYN: curl knocking. Nobody answers, so the kernel knocks again, a bit
more slowly each time, and eventually curl gives up. This is correct behaviour.

You will probably also see the kernel's own ICMPv6 chatter show up as
`protocol not tcp mate`. That one's not a bug either.

The interface disappears when the program exits, so the route goes with it. No cleanup
needed, which is the nicest thing I can say about the current design.

## The plan

- [x] Read raw packets off a TUN device
- [x] Parse the IPv6 header instead of squinting at hex
- [x] Parse the TCP header
- [x] Verify the TCP checksum
- [ ] Write a packet back. Any packet.
- [ ] TCP handshake, by hand, in userspace (this is the part where it stops being cute)
- [ ] Reply `OK` to an HTTP request
- [ ] Do that for one address
- [ ] Do that for 15 million of them
- [ ] Measure RAM, feel things

## Things I already know will hurt

- TCP is a state machine with opinions, and I have to hold all of them myself.
- Checksums. Verifying one turned out fine. Now I have to produce them.
- "Idle servers are free" stops being true the moment someone connects to all of them.

## Credit

The idea, the number 15 million, and the bad influence all belong to the
[kapteko post](https://kapteko.com/15-million-webservers/). Go read that, it actually works.
