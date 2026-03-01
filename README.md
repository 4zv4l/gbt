# Go BitTorrent (WIP)

In construction: learning how to build a dead-simple BitTorrent client from scratch (only stdlib) in Go.  
The goal is to build a minimalist, concurrent CLI client that handles the BitTorrent protocol.

## TODO

- [X] Parse Bencode `.torrent` files
- [X] Request tracker for peers
- [X] Handle TCP Peer Handshakes
- [ ] Asking tracker for more peers
- [ ] Handle Chocking/Unchocking state
- [ ] Download and verify piece hashes
- [ ] Support DHT and magnet link ?
- [ ] Optimize the whole thing

## Testing

Test torrent files (like the Debian ISO) are included in the `torrent/testdata` directorie to ensure the Bencode parser and piece hashing logic work predictably.  
You can run the test with: `go test ./...`.
