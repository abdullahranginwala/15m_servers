# 15m_servers

15 million web servers on one machine, in Go.

Current count: **15,000,000**. Terms and conditions apply, see [How it cheats](#how-it-cheats).

## Why

I read [15 million web servers on one VM](https://kapteko.com/15-million-webservers/) and
had the standard reaction: "that doesn't look so hard". This repo is where I found out
how wrong that was, in Go, one packet at a time.

The trick in the post is that you don't ask the kernel for 15 million sockets. You ask
it for zero. You take a whole subnet, point it at a TUN device, and answer the raw IP
packets yourself. Every address in the subnet becomes a "server", and a server that
nobody is talking to costs you nothing, because it doesn't exist until a packet shows up
looking for it.

## What it does

One process, one loop, one TUN device. For every packet the kernel hands over:

1. Is it IPv6 and TCP? If not, ignore it.
2. Take the low 64 bits of the destination address. That number is the server.
   `::0` is server 0, `::2a` is server 42, and so on up to server 14,999,999.
3. Do just enough TCP to get an HTTP request in and a response out.
4. Write the reply back into the TUN device.

Each server keeps exactly one piece of state: a `uint32` counting how many requests
it has served. 15 million of those is a 60 MB table, and that is the entire memory
cost of the fleet.

```
$ curl 'http://[2001:db8:1234:5678::2a]/'
server 42, request #1
$ curl 'http://[2001:db8:1234:5678::2a]/'
server 42, request #2
$ curl 'http://[2001:db8:1234:5678::2b]/'
server 43, request #1
```

Anything outside the 15 million, or on a port that isn't 80, gets a RST. Politely.

## How it cheats

There is no connection table. Zero bytes per connection, because the server never
remembers that a connection exists. Every reply is computed from the packet that
caused it:

| Client sends | Server replies |
| --- | --- |
| SYN | SYN-ACK, with a sequence number of 1000. Always 1000. |
| Anything carrying data | ACK + the whole HTTP response + FIN, in one packet |
| FIN | ACK |
| A bare ACK | Nothing |
| RST | Nothing |

The sequence numbers the server needs are already in the client's packet (its `ack`
is our `seq`), so there's nothing to store. The client's kernel does all the
remembering and never finds out it's talking to a function.

What this buys in RAM it pays for in correctness:

- No retransmission. If a reply gets lost, the server has already forgotten it sent one.
  On a TUN device on the same machine nothing gets lost, which is carrying this
  whole project.
- The request has to fit in one segment. Every segment with data in it gets a full
  response, so a request split in two would be answered twice.
- It never reads the request. `GET /`, `DELETE /prod`, and the word `hello` all get a 200.
- A fixed initial sequence number is exactly what the TCP RFCs tell you not to do.

So: 15 million things that complete a real TCP handshake with a real kernel and return
a real HTTP response that curl is happy with. Whether that's a "web server" is between
you and your god.

## Running it

Linux only. macOS has no `/dev/net/tun`, so there it just dies on line one.

```sh
go build -o tunservers .
sudo ./tunservers        # add -v to print every packet
```

In a second terminal, bring the interface up and give it a /64:

```sh
sudo ip link set tun0 up
sudo ip -6 addr add 2001:db8:1234:5678:ffff::1/64 dev tun0
```

That address is you. It sits well above server 14,999,999, so you don't end up
being one of your own servers. Everything else in the /64 now routes into the program.

```sh
curl 'http://[2001:db8:1234:5678::2a]/'
```

The interface disappears when the program exits, and the address and route go with it.

## Load test

`loadtest` opens a real TCP connection to every server in turn, sends a request, and
checks that the reply names the right server.

```sh
go run ./loadtest                 # all 15 million, 256 connections at a time
go run ./loadtest -n 100000 -c 64 # or fewer, if you have somewhere to be
```

It prints progress every 5 seconds and a total at the end. The first 10 failures are
logged, the rest are only counted.

`-prefix` is the address of server 0, if you used a different /64.

## Where it got to

- [x] Read raw packets off a TUN device
- [x] Parse the IPv6 and TCP headers instead of squinting at hex
- [x] Verify a TCP checksum, then produce one
- [x] Write a packet back
- [x] TCP handshake, by hand, in userspace
- [x] Reply to an HTTP request
- [x] Do that for 15 million addresses
- [x] A load test that visits every one of them

## Credit

The idea, the number 15 million, and the bad influence all belong to the
[kapteko post](https://kapteko.com/15-million-webservers/). Go read that one. It does
the hard version, with real per-connection state and real benchmarks.
