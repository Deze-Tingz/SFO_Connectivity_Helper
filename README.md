# SFO Connectivity Helper

A relay-based connectivity helper that enables Street Fighter Online (SFO) to connect through NAT/CGNAT without port forwarding.

## Overview

**ONE binary does everything:**
- `sfo-helper server` - Run the relay infrastructure
- `sfo-helper host` - Host a game session
- `sfo-helper join` - Join a game session

## How It Works

1. Someone runs `sfo-helper server` on a VPS/server
2. **Host** runs `sfo-helper host` and gets a join code
3. **Joiner** runs `sfo-helper join --code XXXX-XXXX-XXXX`
4. Both connect through the relay - no port forwarding needed!

## Quick Start

### 1. Run Server (on a VPS or home server)
```bash
sfo-helper server --secret your-secret-key
```

### 2. Host a Game (Player 1)
```bash
sfo-helper host --signal http://YOUR_SERVER:8080 --relay YOUR_SERVER:8443
# Share the displayed JOIN CODE with your friend
```

### 3. Join a Game (Player 2)
```bash
sfo-helper join --code ABCD-EFGH-IJKL --signal http://YOUR_SERVER:8080 --relay YOUR_SERVER:8443
```

## Building

### Prerequisites
- Go 1.21 or later

### Build
```bash
# Windows
go build -o bin/sfo-helper.exe ./cmd/sfo-helper

# Linux
go build -o bin/sfo-helper ./cmd/sfo-helper

# Cross-compile for Linux from Windows
GOOS=linux GOARCH=amd64 go build -o bin/sfo-helper-linux ./cmd/sfo-helper
```

## Project Structure

```
/cmd/sfo-helper         # Unified CLI (server + client)
/internal
  /server
    /session            # Session management
    /auth               # Token authentication
    /ratelimit          # Rate limiting
    /relay              # Relay logic
  /client
    /bridge             # Local port forwarding
    /transport          # Server communication
    /config             # Configuration
/docs                   # Documentation
```

## Configuration

### Server Options (`sfo-helper server`)
| Flag | Default | Description |
|------|---------|-------------|
| `--secret` | - | Shared secret for token signing (required) |
| `--signaling-port` | 8080 | Signaling server port |
| `--relay-port` | 8443 | Relay server port |
| `--session-ttl` | 15 | Session TTL in minutes |
| `--max-session` | 4 | Max session duration in hours |

### Client Options (`sfo-helper host/join`)
| Flag | Default | Description |
|------|---------|-------------|
| `--target` | 127.0.0.1:1626 | Game address |
| `--signal` | localhost:8080 | Signaling server URL |
| `--relay` | localhost:8443 | Relay server address |
| `--debug` | false | Enable debug logging |
| `--skip-wait` | false | Don't wait for game |
| `--code` | - | Join code (required for join) |

## Security

- **Token authentication**: Sessions use HMAC-signed tokens
- **Rate limiting**: Protects against abuse
- **No game modification**: Works alongside the game without changes

## Documentation

- [Windows Quick Start](docs/QUICKSTART_WINDOWS.md)
- [Linux Quick Start](docs/QUICKSTART_LINUX.md)
- [Troubleshooting Guide](docs/TROUBLESHOOTING.md)

## License

MIT License - See LICENSE file for details.

## Contributing

1. Fork the repository
2. Create your feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request
