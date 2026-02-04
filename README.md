# SFO Connectivity Helper

A relay-based connectivity helper that enables Street Fighter Online (SFO) to connect through NAT/CGNAT without port forwarding.

## Overview

This tool creates a tunnel between players using a relay server, allowing gameplay even when:
- Players are behind CGNAT (Carrier-Grade NAT)
- Port forwarding isn't possible
- Complex NAT configurations block direct connections

## How It Works

1. **Host** starts the helper and gets a join code
2. **Joiner** enters the code to connect
3. Both connect to a relay server via outbound TCP
4. The relay pairs them and forwards game traffic
5. End-to-end encryption ensures the relay cannot read game data

## Quick Start

### Host a Game
```bash
sfo-helper host --signal http://server:8080 --relay server:8443
# Share the displayed JOIN CODE with your friend
```

### Join a Game
```bash
sfo-helper join --code ABCD-EFGH-IJKL --signal http://server:8080 --relay server:8443
```

## Building

### Prerequisites
- Go 1.21 or later
- Docker (for servers)

### Build Client
```bash
# Windows
cd client && go build -o bin/sfo-helper.exe ./cmd/sfo-helper

# Linux
cd client && go build -o bin/sfo-helper ./cmd/sfo-helper

# Cross-compile
GOOS=windows GOARCH=amd64 go build -o bin/sfo-helper.exe ./cmd/sfo-helper
GOOS=linux GOARCH=amd64 go build -o bin/sfo-helper ./cmd/sfo-helper
```

### Build & Run Servers
```bash
cd deploy
cp .env.example .env
# Edit .env with your secret
docker-compose up -d
```

## Project Structure

```
/client                 # Client helper application
  /cmd/sfo-helper       # CLI entry point
  /internal
    /bridge             # Local port forwarding
    /transport          # Server communication
    /crypto             # End-to-end encryption
    /config             # Configuration
    /platform           # OS-specific code

/server                 # Server components
  /cmd/signaling        # Signaling server (session management)
  /cmd/relay            # Relay server (traffic forwarding)
  /internal
    /session            # Session store
    /auth               # Token authentication
    /ratelimit          # Rate limiting
    /relay              # Relay logic

/deploy                 # Deployment files
  docker-compose.yml
  Dockerfile.signaling
  Dockerfile.relay

/docs                   # Documentation
  QUICKSTART_WINDOWS.md
  QUICKSTART_LINUX.md
  TROUBLESHOOTING.md
```

## Configuration

### Client Options
| Flag | Default | Description |
|------|---------|-------------|
| `--target` | 127.0.0.1:1626 | Game address |
| `--signal` | - | Signaling server URL |
| `--relay` | - | Relay server address |
| `--debug` | false | Enable debug logging |
| `--skip-wait` | false | Don't wait for game |

### Server Environment Variables
| Variable | Default | Description |
|----------|---------|-------------|
| `SIGNALING_PORT` | 8080 | Signaling server port |
| `RELAY_PORT` | 8443 | Relay server port |
| `SIGNALING_SECRET` | - | Shared secret for tokens |
| `SESSION_TTL_MINUTES` | 15 | Session expiration time |
| `MAX_SESSION_HOURS` | 4 | Max session duration |

## Security

- **End-to-end encryption**: Game traffic is encrypted using NaCl box
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
