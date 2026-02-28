# Go BitTorrent (WIP)

In construction: learning how to build a dead-simple BitTorrent client from scratch (only stdlib) in Go.  
The goal is to build a minimalist, concurrent CLI client that handles the BitTorrent protocol.

## Current Phase: 

### 1. The Base Client:

- [X] Parse Bencode `.torrent` files
- [ ] Communicate with HTTP Trackers
- [ ] Handle TCP Peer Handshakes
- [ ] Download and verify piece hashes

## Testing

Test torrent files (like the Debian ISO) are included in the `torrent/testdata` directorie to ensure the Bencode parser and piece hashing logic work predictably.  
You can run the test with: `go test ./...`.
