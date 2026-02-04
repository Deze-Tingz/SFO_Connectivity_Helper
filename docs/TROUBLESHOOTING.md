# SFO Connectivity Helper - Troubleshooting Guide

## Diagnostic Command

Always start troubleshooting by running diagnostics:
```
sfo-helper diagnose --signal http://YOUR_SERVER:8080 --relay YOUR_SERVER:8443
```

This checks:
1. Local game port status
2. Signaling server connectivity
3. Relay server connectivity
4. DNS resolution

## Common Issues

### 1. "Waiting for game..." never completes

**Cause**: The game isn't listening on port 1626.

**Solutions**:
- Start Street Fighter Online before running the helper
- Verify the game is configured for the correct port
- Check if another application is using port 1626
- On Windows: `netstat -an | findstr 1626`
- On Linux: `netstat -tlnp | grep 1626`

**Workaround**: Use `--skip-wait` flag to bypass game detection.

### 2. "Failed to connect to signaling server"

**Cause**: Cannot reach the signaling server.

**Solutions**:
- Verify the server URL is correct
- Check your internet connection
- Verify the server is running: `curl http://YOUR_SERVER:8080/health`
- Check if a firewall is blocking outbound connections

### 3. "Failed to connect to relay"

**Cause**: Cannot reach the relay server.

**Solutions**:
- Verify the relay address is correct
- Test connectivity: `telnet YOUR_SERVER 8443` or `nc -zv YOUR_SERVER 8443`
- Check if your network blocks non-standard ports
- Try using port 443 if available (some relays support this)

### 4. "Invalid or expired join code"

**Cause**: The join code is wrong or has expired.

**Solutions**:
- Double-check the code (case-insensitive, dashes optional)
- Codes expire after 15 minutes
- Ask the host to create a new session

### 5. "Session already has a joiner"

**Cause**: Someone else joined the session, or there was a previous failed join.

**Solutions**:
- Ask the host to create a new session
- Wait for the old session to expire (15 minutes)

### 6. "Rate limit exceeded"

**Cause**: Too many requests in a short time.

**Solutions**:
- Wait 1-2 minutes before trying again
- Don't spam create/join requests

### 7. Connection drops after connecting

**Cause**: Network instability or timeout.

**Solutions**:
- Check your internet connection stability
- Sessions have a maximum duration (usually 4 hours)
- The game may have disconnected on one side

### 8. "Another instance is already running"

**Cause**: Single-instance lock is held.

**Solutions**:
- Close any other sfo-helper processes
- On Windows: Check Task Manager for sfo-helper.exe
- On Linux: `pkill sfo-helper`
- Delete lock file if process crashed:
  - Linux: `rm /tmp/sfo-helper-*.lock`

## Network-Specific Issues

### Behind Corporate Firewall/Proxy

If you're on a restricted network that only allows port 80/443:
- Request relay deployment on port 443
- Some networks require proxy configuration

### CGNAT (Carrier-Grade NAT)

This helper is specifically designed for CGNAT situations:
- Always uses relay mode (outbound connections only)
- No port forwarding required
- Works through most NAT configurations

### VPN Interference

If using a VPN:
- Try with VPN disabled to test
- If VPN is required, ensure it allows the relay ports
- Some VPNs interfere with local port detection

## Server-Side Issues

If you're running your own servers:

### Signaling Server Not Starting
```bash
# Check logs
docker-compose logs signaling

# Common issues:
# - Port already in use
# - Invalid configuration
```

### Relay Server Not Pairing
```bash
# Check logs
docker-compose logs relay

# Common issues:
# - Token validation failing (check SIGNALING_SECRET matches)
# - Clients timing out before pairing
```

## Debug Mode

For detailed logging, use the `--debug` flag:
```
sfo-helper host --debug --signal http://... --relay ...
```

This shows:
- All network connections
- State transitions
- Detailed error messages

## Getting Help

If you're still stuck:

1. Run diagnostics and save output:
   ```
   sfo-helper diagnose > diagnostic.txt 2>&1
   ```

2. Run with debug mode and save output:
   ```
   sfo-helper host --debug > debug.txt 2>&1
   ```

3. Open an issue with:
   - Your OS version
   - diagnostic.txt contents
   - debug.txt contents (sanitize any sensitive info)
   - Steps to reproduce the issue
