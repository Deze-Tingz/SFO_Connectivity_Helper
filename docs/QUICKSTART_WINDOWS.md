# SFO Connectivity Helper - Windows Quick Start

## Prerequisites
- Windows 10 or Windows 11
- Street Fighter Online installed
- The game should be configured to listen on port 1626 (default)

## Installation

1. Download `sfo-helper.exe` from the releases page
2. Place it in any folder (e.g., `C:\Games\SFO-Helper\`)
3. No installation required - it's a single executable

## Windows Firewall Setup

The first time you run the helper, Windows may ask to allow it through the firewall. Click "Allow access" for private networks.

If you need to manually add firewall rules, run PowerShell as Administrator:

```powershell
# Allow sfo-helper through firewall
New-NetFirewallRule -DisplayName "SFO Helper" -Direction Inbound -Program "C:\Games\SFO-Helper\sfo-helper.exe" -Action Allow
```

## Usage

### Hosting a Game

1. Start Street Fighter Online first
2. Open Command Prompt or PowerShell
3. Navigate to the helper folder:
   ```cmd
   cd C:\Games\SFO-Helper
   ```
4. Run the host command:
   ```cmd
   sfo-helper.exe host --signal http://YOUR_SERVER:8080 --relay YOUR_SERVER:8443
   ```
5. Wait for "Game detected!" message
6. Share the displayed JOIN CODE with your friend

### Joining a Game

1. Start Street Fighter Online first
2. Open Command Prompt or PowerShell
3. Navigate to the helper folder
4. Run the join command with the code from your friend:
   ```cmd
   sfo-helper.exe join --code ABCD-EFGH-IJKL --signal http://YOUR_SERVER:8080 --relay YOUR_SERVER:8443
   ```
5. Wait for "CONNECTED (RELAYED)" message
6. You're connected! Play the game normally.

## Command Reference

```cmd
# Host a game
sfo-helper.exe host [options]

# Join a game
sfo-helper.exe join --code XXXX-XXXX-XXXX [options]

# Run diagnostics
sfo-helper.exe diagnose [options]

# Common options:
--target HOST:PORT    Game address (default: 127.0.0.1:1626)
--signal URL          Signaling server URL
--relay HOST:PORT     Relay server address
--debug               Enable debug output
--skip-wait           Don't wait for game to start
```

## Troubleshooting

### "Game not detected"
- Make sure SFO is running and listening on port 1626
- Run `sfo-helper.exe diagnose` to check connectivity

### "Connection refused"
- Check if servers are running and reachable
- Verify firewall isn't blocking the connection
- Try running as Administrator

### "Invalid join code"
- Double-check the code was entered correctly
- Codes expire after 15 minutes
- Ask the host to create a new session

## Antivirus Notes

Some antivirus software may flag the helper because:
- It creates network connections
- It's an unsigned executable

The helper is safe - it only:
- Opens outbound TCP connections to the relay server
- Forwards traffic between the game and relay
- Does NOT modify any game files or system settings

If your AV flags it, add an exception for `sfo-helper.exe`.
