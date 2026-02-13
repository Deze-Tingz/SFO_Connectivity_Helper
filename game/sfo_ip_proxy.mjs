// SFO IP Rewrite Proxy — transparent SMUS proxy that rewrites public IP to LAN IP
// Fixes NAT hairpin by making the game advertise a LAN-routable IP in room names
//
// Architecture:
//   Game (host PC) → this proxy (127.0.0.1) → real SMUS server (99.240.97.63:27015)
//   Server responses have public IP replaced with LAN IP (same byte length)
//
// Ports:
//   1626  — standard SMUS port (bFirewall=0, default game mode)
//   27015 — firewall mode port (bFirewall=1, SFO Haven/Tournament)
//   80    — HTTP firewall check (game fetches /ip.html for PlayerFirewallOK)
//   All forward to the real server on 27015
//
// Setup (host PC, LAN mode):
//   1. Add hosts file entries: "127.0.0.1 dor.gotdns.com sfohaven.gotdns.com ..."
//   2. Run this proxy: node sfo_ip_proxy.mjs
//   3. Launch the game
//   4. Room names now contain LAN IP — other LAN players connect to 192.168.1.8:1626
//
// Root cause (from Lingo decompilation):
//   fnBHostGame.ls:40 bakes myIP (public IP from system.user.getAddress) into room name
//   fnNewP2P.ls:73-79 has txtIP2 override for P2P but NOT room name
//   This proxy rewrites the IP in server responses so myIP = LAN IP

import { createServer, connect } from 'net';
import { createServer as createHttpServer } from 'http';

// ─── Configuration ─────────────────────────────────────────────────────────
// Update these values for your network setup

const LISTEN_HOST = '127.0.0.1';     // Localhost — hosts file redirects game here
const LISTEN_PORTS = [1626, 27015];  // Both game ports (standard + firewall mode)
const SMUS_SERVER = '99.240.97.63';  // Real SMUS game server
const SMUS_PORT = 27015;

// IP replacement — MUST be same byte length
// Your public IP (14 chars):
const PUBLIC_IP = '199.204.234.98';
// Host PC's LAN IP, zero-padded to 14 chars:
// Windows inet_addr("192.168.001.08") → 192.168.1.8  (leading zeros OK)
const LAN_IP    = '192.168.001.08';

// HTTP firewall check response IP (no padding needed here)
const LAN_IP_DISPLAY = '192.168.1.8';

// ─── Validation ────────────────────────────────────────────────────────────

const PUBLIC_IP_BUF = Buffer.from(PUBLIC_IP, 'ascii');
const LAN_IP_BUF    = Buffer.from(LAN_IP, 'ascii');

if (PUBLIC_IP_BUF.length !== LAN_IP_BUF.length) {
  console.error(`FATAL: IP lengths don't match! "${PUBLIC_IP}" (${PUBLIC_IP_BUF.length}) vs "${LAN_IP}" (${LAN_IP_BUF.length})`);
  process.exit(1);
}

// ─── IP Rewrite Engine ─────────────────────────────────────────────────────

let connectionId = 0;

function replaceIP(data) {
  let replaced = 0;
  let offset = 0;
  while (true) {
    const idx = data.indexOf(PUBLIC_IP_BUF, offset);
    if (idx === -1) break;
    LAN_IP_BUF.copy(data, idx);
    replaced++;
    offset = idx + LAN_IP_BUF.length;
  }
  return replaced;
}

// ─── TCP Proxy (SMUS) ─────────────────────────────────────────────────────

function createProxyServer(listenPort) {
  const server = createServer((clientSocket) => {
    const connId = ++connectionId;
    const clientAddr = `${clientSocket.remoteAddress}:${clientSocket.remotePort}`;
    console.log(`[${connId}] Game connected from ${clientAddr} on port ${listenPort}`);

    const serverSocket = connect(SMUS_PORT, SMUS_SERVER, () => {
      console.log(`[${connId}] Connected to SMUS server ${SMUS_SERVER}:${SMUS_PORT}`);
    });

    // Server → Client (rewrite public IP to LAN IP)
    serverSocket.on('data', (data) => {
      const buf = Buffer.from(data);
      const count = replaceIP(buf);
      if (count > 0) {
        console.log(`[${connId}] S->C: Replaced ${count} IP occurrence(s) in ${buf.length} bytes`);
        const text = buf.toString('utf8').replace(/[^\x20-\x7e]/g, '.');
        const ipIdx = text.indexOf(LAN_IP);
        if (ipIdx !== -1) {
          const context = text.substring(Math.max(0, ipIdx - 30), Math.min(text.length, ipIdx + 50));
          console.log(`[${connId}]   Context: ...${context}...`);
        }
      }
      clientSocket.write(buf);
    });

    // Client → Server (pass through unmodified)
    clientSocket.on('data', (data) => {
      serverSocket.write(data);
    });

    clientSocket.on('error', (err) => {
      console.log(`[${connId}] Client error: ${err.message}`);
      serverSocket.destroy();
    });

    serverSocket.on('error', (err) => {
      console.log(`[${connId}] Server error: ${err.message}`);
      clientSocket.destroy();
    });

    clientSocket.on('close', () => {
      console.log(`[${connId}] Client disconnected`);
      serverSocket.destroy();
    });

    serverSocket.on('close', () => {
      console.log(`[${connId}] Server disconnected`);
      clientSocket.destroy();
    });
  });

  server.on('error', (err) => {
    if (err.code === 'EADDRINUSE') {
      console.error(`Port ${listenPort} already in use. Kill existing process or use a different port.`);
    } else {
      console.error(`Server error on port ${listenPort}: ${err.message}`);
    }
    process.exit(1);
  });

  return server;
}

// ─── HTTP Server (firewall check) ─────────────────────────────────────────
// Game fetches http://sfohaven.gotdns.com/ip.html
// Must respond with text containing "ok " and "." for PlayerFirewallOK = 1

const httpServer = createHttpServer((req, res) => {
  console.log(`[HTTP] ${req.method} ${req.url} from ${req.socket.remoteAddress}`);
  res.writeHead(200, { 'Content-Type': 'text/html' });
  res.end(`ok ${LAN_IP_DISPLAY}`);
});

httpServer.on('error', (err) => {
  if (err.code === 'EADDRINUSE') {
    console.error('Port 80 already in use - HTTP firewall check server skipped');
    console.error('  (game may show "Firewall blocked" but P2P still works)');
  } else if (err.code === 'EACCES') {
    console.error('Port 80 requires admin/elevated permissions - HTTP firewall check skipped');
    console.error('  (run as Administrator for full functionality)');
  } else {
    console.error(`HTTP server error: ${err.message}`);
  }
});

// ─── Startup ───────────────────────────────────────────────────────────────

console.log('');
console.log('  SFO IP Rewrite Proxy');
console.log('  ====================');
console.log(`  Upstream:  ${SMUS_SERVER}:${SMUS_PORT}`);
console.log(`  Rewrite:   "${PUBLIC_IP}" -> "${LAN_IP}"`);
console.log(`  Firewall:  "ok ${LAN_IP_DISPLAY}"`);
console.log(`  (${PUBLIC_IP_BUF.length} bytes -> ${LAN_IP_BUF.length} bytes, same length)`);
console.log('');

let started = 0;
const totalServers = LISTEN_PORTS.length + 1;

httpServer.listen(80, LISTEN_HOST, () => {
  console.log(`  [OK] HTTP  ${LISTEN_HOST}:80`);
  started++;
  if (started === totalServers) {
    console.log('\n  All ports active. Waiting for game connections...\n');
  }
});

for (const port of LISTEN_PORTS) {
  const server = createProxyServer(port);
  server.listen(port, LISTEN_HOST, () => {
    console.log(`  [OK] SMUS  ${LISTEN_HOST}:${port}`);
    started++;
    if (started === totalServers) {
      console.log('\n  All ports active. Waiting for game connections...\n');
    }
  });
}
