# NAT Hairpin Root Cause — Lingo Source Analysis

**Date:** 2026-02-13
**Evidence:** `Critical_Port_Find.png` (screenshot from analysis session)

## The Problem

When two players are on the same LAN (e.g., 192.168.1.8 and 192.168.1.10), the joining player sees the host's **public IP** (199.204.234.98) in the game lobby. Connecting to this public IP from the same LAN hits the **NAT hairpin wall** — most consumer routers don't support hairpin NAT, so the connection fails.

Players on different networks connect fine because they're supposed to use the public IP.

## Game Room Format (plaintext lobby)

```
@Player69 1626 199.204.234.98 26a Player69's game
 ^name    ^port ^public IP     ^ver ^title
```

The public IP is embedded directly in the room broadcast string.

## Root Cause — `fnBHostGame.ls` line 40

```lingo
sServerChan = "@" & sPlayer1Name & " " & myPort & " " & myIP & " " & theVersion & " " & member("txtServerNameIn").text
```

- `myIP` comes from `system.user.getAddress` — the SMUS server tells the client its public IP
- This value is **hardcoded into the room name**
- You cannot change what the server reports

## The Override That Almost Works — `fnNewP2P.ls` lines 73-79

```lingo
sTEMPIPAddress = myIP
if member("txtIP2").text <> "AUTO" then
  sTEMPIPAddress = member("txtIP2").text
end if
```

- `txtIP2` overrides the **P2P connection target** but NOT the room name
- So the actual connection can use a LAN IP, but the lobby still advertises the public IP
- Other clients parse the room name to get the connection IP

## Fix Approaches

### Option 1: Patch Lingo source (cleanest)

Modify `fnBHostGame.ls` to use `sTEMPIPAddress` instead of `myIP`:

```lingo
sServerChan = "@" & sPlayer1Name & " " & myPort & " " & sTEMPIPAddress & " " & ...
```

Then set `txtIP2 = 192.168.1.8` on the host. The room name would advertise the LAN IP. Director 11 (installed in Win7 VM) can recompile the cast.

### Option 2: IP rewrite proxy (current solution)

`sfo_ip_proxy.mjs` sits between the game and the SMUS server, replacing public IP bytes with LAN IP bytes in server responses. This makes `system.user.getAddress` return the LAN IP, so `myIP` is already correct before it reaches `fnBHostGame.ls`.

**Pros:** No game modification needed
**Cons:** Requires hosts file redirect + Node.js running

### Option 3: TCP relay on joining PC

Node.js script on the remote PC intercepts connections to the public IP and redirects to the LAN IP. Only works for LAN scenarios.

## Account Metadata

Player "enters" broadcasts carry optional metadata:
- `[PXVIP03V]` — VIP tier (01, 03, etc.) with V flag (verified?)
- `(rank:9)` — Player rank number
- `creed:Streetfighter` — Faction/clan tag ("creed" system)
- Anonymous/guest accounts send NO VIP/rank/creed data
