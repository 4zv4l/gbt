# Go BitTorrent (WIP)

In construction: learning how to build a dead-simple BitTorrent client from scratch (only stdlib) in Go.  
The goal is to build a minimalist, concurrent CLI client that handles the BitTorrent protocol.

## TODO

- [X] Parse Bencode `.torrent` files
- [X] Request tracker for peers
- [X] Handle TCP Peer Handshakes
- [X] Asking tracker for more peers
- [X] Handle Chocking/Unchocking state
- [X] Download and verify piece hashes
- [X] Restart download after a crash or connection issue
- [ ] Support DHT and magnet link ?
- [ ] Optimize the whole thing

## Testing

Test torrent files (like the Debian ISO) are included in the `torrent/testdata` directorie to ensure the Bencode parser and piece hashing logic work predictably.  
You can run the test with: `go test ./...`.
