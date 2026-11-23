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

It opens `/dev/net/tun`, attaches to `tun0`, and prints every packet the kernel hands
it as hex. That's the whole program. It's 37 lines and 12 of them are error handling.

It does not reply. It does not parse. It is, strictly speaking, a very attentive
`/dev/null`.

## Running it

Linux only. macOS has no `/dev/net/tun`, so there it just dies on line one.

```sh
go build -o tunservers .
sudo ./tunservers
```

In a second terminal, bring the interface up and give it sixteen million addresses
to worry about:

```sh
sudo ip link set tun0 up
sudo ip route add 10.0.0.0/8 dev tun0
```

Now ping any of them:

```sh
ping 10.42.0.69
```

The first terminal fills up with something like:

```
attached to tun0, fd: 3
84 bytes: 45000054a1b240004001...
84 bytes: 45000054a2110000400...
```

`45` is IPv4 with a 20 byte header, `0054` is 84 bytes total, and somewhere in there is
an ICMP echo request that will never get its echo. Ping reports 100% packet loss. This
is correct behaviour.

The interface disappears when the program exits, so the route goes with it. No cleanup
needed, which is the nicest thing I can say about the current design.

## The plan

- [x] Read raw packets off a TUN device
- [ ] Parse the IP header instead of squinting at hex
- [ ] Answer a ping, so at least one thing on this network has manners
- [ ] TCP handshake, by hand, in userspace (this is the part where it stops being cute)
- [ ] Reply `OK` to an HTTP request
- [ ] Do that for one address
- [ ] Do that for 15 million of them
- [ ] Measure RAM, feel things

## Things I already know will hurt

- TCP is a state machine with opinions, and I have to hold all of them myself.
- Checksums.
- "Idle servers are free" stops being true the moment someone connects to all of them.

## Credit

The idea, the number 15 million, and the bad influence all belong to the
[kapteko post](https://kapteko.com/15-million-webservers/). Go read that, it actually works.
