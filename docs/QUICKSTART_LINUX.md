# SFO Connectivity Helper - Linux Quick Start

## Prerequisites
- Linux (Ubuntu 20.04+, Debian 11+, Fedora 35+, or similar)
- Street Fighter Online running (via Wine/Proton)
- Game configured to listen on port 1626

## Installation

1. Download `sfo-helper` from the releases page
2. Make it executable:
   ```bash
   chmod +x sfo-helper
   ```
3. Optionally move to a directory in your PATH:
   ```bash
   sudo mv sfo-helper /usr/local/bin/
   ```

## Firewall Setup

### Ubuntu/Debian (ufw)
```bash
# Allow the helper (if needed for local network)
sudo ufw allow from 192.168.0.0/16 to any port 1626 proto tcp
```

### Fedora/RHEL (firewalld)
```bash
# Allow on local network
sudo firewall-cmd --add-port=1626/tcp --permanent
sudo firewall-cmd --reload
```

Note: The helper primarily uses outbound connections, so firewall rules are usually not required for basic functionality.

## Usage

### Hosting a Game

1. Start Street Fighter Online first
2. Open a terminal
3. Run the host command:
   ```bash
   sfo-helper host --signal http://YOUR_SERVER:8080 --relay YOUR_SERVER:8443
   ```
4. Wait for "Game detected!" message
5. Share the displayed JOIN CODE with your friend

### Joining a Game

1. Start Street Fighter Online first
2. Open a terminal
3. Run the join command:
   ```bash
   sfo-helper join --code ABCD-EFGH-IJKL --signal http://YOUR_SERVER:8080 --relay YOUR_SERVER:8443
   ```
4. Wait for "CONNECTED (RELAYED)" message
5. Play!

## Running as a Service (Optional)

Create a systemd user service for automatic startup:

```bash
mkdir -p ~/.config/systemd/user

cat > ~/.config/systemd/user/sfo-helper.service << 'EOF'
[Unit]
Description=SFO Connectivity Helper
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sfo-helper host --signal http://YOUR_SERVER:8080 --relay YOUR_SERVER:8443 --skip-wait
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF

# Enable and start
systemctl --user enable sfo-helper
systemctl --user start sfo-helper

# Check status
systemctl --user status sfo-helper
```

## Command Reference

```bash
# Host a game
sfo-helper host [options]

# Join a game
sfo-helper join --code XXXX-XXXX-XXXX [options]

# Run diagnostics
sfo-helper diagnose [options]

# Common options:
--target HOST:PORT    Game address (default: 127.0.0.1:1626)
--signal URL          Signaling server URL
--relay HOST:PORT     Relay server address
--debug               Enable debug output
--skip-wait           Don't wait for game to start
```

## Troubleshooting

### "Game not detected"
- Verify SFO is running: `netstat -tlnp | grep 1626`
- If using Wine, ensure the game binds to localhost
- Run `sfo-helper diagnose` for full diagnostics

### "Permission denied"
- Ensure the binary is executable: `chmod +x sfo-helper`
- Check if another instance is running: `pgrep sfo-helper`

### "Connection refused"
- Verify servers are running and accessible
- Check firewall: `sudo ufw status` or `sudo firewall-cmd --list-all`
- Test connectivity: `nc -zv YOUR_SERVER 8443`

### Multiple Network Interfaces
If you have multiple network interfaces and the game binds to a specific one:
```bash
sfo-helper host --target 192.168.1.100:1626
```
