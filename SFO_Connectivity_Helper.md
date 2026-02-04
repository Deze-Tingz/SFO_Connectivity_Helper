You are Claude Code. Build a professional-grade “SFO Connectivity Helper” that enables a legacy game (Street Fighter Online) to connect reliably without router port forwarding by tunneling its network traffic through an outbound-only overlay with relay fallback.

NON-NEGOTIABLE CONSTRAINTS
- Do NOT modify, patch, inject into, or reverse-engineer the game binary.
- Helper is a separate program run alongside the game.
- Provide native clients for BOTH:
  - Windows 10/11 (2026)
  - Linux (modern distros: Ubuntu/Debian/Fedora)
- No cheating, no payload manipulation beyond transport.
- Primary target port: 1626.
- Start with TCP support (netstat evidence shows TCP LISTENING on 1626). Keep design extensible for UDP later.

REALITY CHECK
- CGNAT cannot be “fixed” by port forwarding. Reliability requires outbound tunnels + relay fallback.
- MVP should prioritize “Always Relay” mode (100% connectivity) over complex NAT traversal. Add optional direct mode later.

CORE GOAL (User Workflow)
1) Host runs helper + SFO.
2) Host gets a join code.
3) Joiners run helper + SFO, enter join code.
4) Helper reports: CONNECTED (RELAYED) or CONNECTED (DIRECT).
5) No router configuration required.

HIGH-LEVEL ARCHITECTURE (MVP → v1)
Build 3 components:
A) Signaling server (small): session creation, join codes, auth tokens.
B) Relay server (required for reliability): forwards encrypted streams over a single egress-friendly port (443/8443).
C) Client helper (Windows + Linux): bridges local game socket to remote tunnel.

IMPORTANT: NO “SFO detection” by name is required.
- Use port-based attachment, not process inspection.
- The helper treats “whatever is on target port” as the game.

CLIENT MODES (must implement both)
MODE ATTACH (default):
- The game listens on :1626 itself.
- Helper does NOT bind :1626.
- Helper forwards remote traffic to 127.0.0.1:1626 (or LAN IP:1626).
- This avoids conflicts and is legally/technically clean.

MODE LISTEN (fallback):
- If the game cannot/does not listen as expected, helper can listen on a chosen local port (e.g., 1627) and user configures SFO Local Port=1627.
- Provide docs to set SFO Local IP to the appropriate local interface (usually 127.0.0.1 or the VPN interface).

STARTUP RACE CONDITIONS (MANDATORY FIXES)
Implement a robust startup state machine to avoid races where helper starts before SFO or vice versa.

Requirements:
1) Port Probe Loop:
   - On HOST in ATTACH mode: repeatedly probe whether 127.0.0.1:1626 is LISTENING (with timeout window).
   - If not listening yet, show “WAITING FOR GAME” and continue probing.
   - Once listening, transition to “READY” and accept remote connections.
2) Connection Backoff:
   - If forwarding fails because SFO is not yet bound, queue/hold remote connection briefly (grace period) and retry connecting to local port with exponential backoff.
3) Atomic Session Ownership:
   - Host session creation must occur once; re-use session token on reconnect.
4) Single-Instance Lock:
   - Prevent two helper instances from fighting over resources (Windows mutex / Linux lock file).
5) Clean Shutdown:
   - On CTRL+C or service stop: close tunnels, end relay pairing, flush logs, release locks.
6) Health Checks:
   - Show local readiness (LISTEN detected) and remote readiness (relay connected).

TRANSPORT & SECURITY (MVP)
- Use ALWAYS RELAY mode by default for reliability:
  - Clients connect outbound to relay on TCP 443/8443.
  - Relay pairs host+joiner using auth token from signaling.
- Encryption:
  - Use Noise protocol OR TLS 1.3 with ephemeral keys derived from the join code.
  - Choose ONE approach and implement end-to-end encryption so relay cannot read payload.
- Authentication:
  - Join code must map to a session ID + short-lived join token.
  - Relay accepts connections only with valid signed tokens.
- Abuse protections:
  - Rate limit join attempts.
  - Session TTL and cleanup.
  - Random, unguessable codes (e.g., 10–12 chars base32).

EDGE CASE FIXES (MANDATORY)
1) Local Port Already Used:
   - If 1626 is in use by another process and SFO is not running, report clearly and suggest steps.
2) Wrong Bind Address:
   - Support target selection: 127.0.0.1 vs LAN IP.
   - Auto-detect whether SFO listens on 0.0.0.0:1626 or a specific LAN IP and adapt.
3) Antivirus / EDR false positives (Windows):
   - Keep binary signed instructions in docs (optional).
   - Avoid process injection, kernel drivers, raw packet capture.
   - Use standard sockets only; no WinDivert unless explicitly added later.
   - Provide “AV-friendly mode” flag (disables advanced features, uses plain TLS, standard ports).
4) Windows Firewall / Linux Firewall:
   - Provide setup steps and automated helpers.
   - Windows: add inbound allow rule for local private networks if needed (though relay is outbound, local listening/loopback may still be flagged).
   - Linux: ufw/firewalld guidance.
   - Provide a “diagnose” command that tests:
     - Local port readiness
     - Outbound connectivity to signaling/relay
     - DNS resolution
5) Corporate Proxy / Restricted networks:
   - Offer relay over 443 with WebSocket fallback (optional).
6) NAT/CGNAT:
   - Always relay works regardless; document this clearly.

PLATFORM-SPECIFIC REQUIREMENTS
Windows client:
- Build a single .exe (no installer required for MVP).
- Optional: add a minimal tray icon later (not required).
- Implement single-instance via named mutex.
- Provide PowerShell scripts to add firewall rules and diagnostics.

Linux client:
- Provide a single binary + systemd service unit (optional).
- Implement single-instance via lock file in /tmp or /var/run.
- Provide shell scripts for ufw/firewalld rule checks and diagnostics.

COMMANDS (MUST SUPPORT)
- sfo-helper host --target 127.0.0.1:1626 --relay wss://relay.example --signal https://signal.example
- sfo-helper join --code ABCD-EFGH-IJKL --target 127.0.0.1:1626
- sfo-helper status
- sfo-helper diagnose (runs all checks and prints actionable output)
- Flags:
  --always-relay (default true)
  --debug
  --bind-interface (optional)
  --local-listen-port (for LISTEN mode)

SIGNALING SERVER (IMPLEMENT)
- Minimal REST or WebSocket API:
  - POST /session/create -> {code, hostToken, sessionId}
  - POST /session/join -> {joinToken, sessionId}
  - GET /session/status -> {connected, mode}
- In-memory store + TTL (Redis optional later).
- Rate limits per IP.

RELAY SERVER (IMPLEMENT)
- Single port (443/8443).
- Clients connect outbound.
- Relay pairs two clients by sessionId + token.
- Relay forwards encrypted bytes bidirectionally.
- Enforce timeouts and max session duration.

OBSERVABILITY / UX (MUST HAVE)
- Clear console states:
  WAITING_FOR_GAME → READY → CONNECTING_RELAY → CONNECTED(RELAYED/DIRECT)
- Print:
  - Session code (host)
  - Bytes in/out
  - Last error
  - Retry countdown
- Logs:
  - Rotating file logs
  - Redact secrets/tokens in logs

DELIVERABLES
Repo layout:
  /client
    /cmd/sfo-helper
    /bridge
    /transport
    /crypto
    /platform/windows
    /platform/linux
  /server
    /signaling
    /relay
  /deploy
    docker-compose.yml
    .env.example
  /docs
    QUICKSTART_WINDOWS.md
    QUICKSTART_LINUX.md
    TROUBLESHOOTING.md
    SECURITY.md

DOCS MUST INCLUDE
- Step-by-step Windows + Linux usage
- Firewall instructions:
  - Windows Defender Firewall allow-list commands (PowerShell)
  - ufw/firewalld commands
- Antivirus notes: what’s safe, what to whitelist (binary path), what NOT to do
- Race condition handling explanation: “start helper first or SFO first—both work”
- Common errors and exact fixes

IMPLEMENTATION PLAN (STRICT)
Phase 1 (MVP: relay-only, TCP only):
- Build signaling server + relay server + client bridge
- Always relay mode default
- Robust startup probing and retries
- Diagnose command
- Docker compose for servers
- Windows and Linux builds

Phase 2 (Hardening):
- Session cleanup, rate limiting, better metrics
- Optional systemd and Windows service
- Better UX

Phase 3 (Optional: direct mode):
- Candidate checks and direct attempt
- Always keep relay fallback

NOW EXECUTE
- Do not ask questions; pick sane defaults:
  - Relay port 8443
  - Signaling REST over 8080
  - TLS for client↔relay plus end-to-end session keys derived from join code
- Start by generating repo skeleton and docs.
- Then implement Phase 1 end-to-end with build/run commands.
- Provide exact build commands for Windows and Linux.
- Keep code clean, commented, production-minded (timeouts, backoff, limits).