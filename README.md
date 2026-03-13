# GBT - Go BitTorrent

`GBT` is a minimalist BitTorrent cli client made in Go.  
I made this project to learn more about how the BitTorrent protocol works and
because I wanted a dead simple cli client with no dependency
(and as a fun challenge, only using Go's stdlib)
that can easily be cross-compiled to any device.

## Getting started

Its as simple as:

```bash
$ git clone https://github.com/4zv4l/gbt
$ cd gbt
$ go build -o bin/ ./cmd/...

# usage
$ ./bin/gbt
Usage of ./bin/gbt:
  -file string
        torrent file
  -peer string
        IP:Port of a direct peers (separated by ',') to connect to
  -pipeline int
        pipeline size for peer request (default 5)
  -port int
        port to listen for peers (<=0 to not listen) (default -1)
  -seed
        keep running and seeding after download completes

# start downloading debian iso
$ ./bin/gbt -file ./torrent/testdata/debian-13.3.0-amd64-netinst.iso.torrent
2026/03/13 12:00:35 === Info for debian-13.3.0-amd64-netinst.iso ===
2026/03/13 12:00:35 Announce:    http://bttracker.debian.org:6969/announce
2026/03/13 12:00:35 InfoHash:    86f635034839f1ebe81ab96bee4ac59f61db9dde
2026/03/13 12:00:35 PieceLength: 256.00 KB
2026/03/13 12:00:35 TotalLength: 754.00 MB
2026/03/13 12:00:35 Piece Count: 3016
2026/03/13 12:00:35 Peer ID:     4a88c6125063a8509897cf272f0c2e8e68c1d6b0
2026/03/13 12:00:35 Files (1):
2026/03/13 12:00:35   [0] debian-13.3.0-amd64-netinst.iso (754.00 MB)
Download completed in 16.057386907s
```

## TODO

- [X] Parse Bencode `.torrent` files
- [X] Request tracker for peers
- [X] Handle TCP Peer Handshakes
- [X] Asking tracker for more peers
- [X] Handle Choking/Unchoking state
- [X] Download and verify piece hashes
- [X] Restart download after a crash or connection issue
- [ ] Support DHT and magnet link ?
- [ ] Optimize the whole thing
