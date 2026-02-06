# SFO Local Relay Server - Network Helper
# Pure PowerShell - no external dependencies

param(
    [int]$LocalPort = 1626,
    [string]$Mode = "menu"
)

function Get-LocalIPs {
    $ips = [System.Collections.ArrayList]@()
    try {
        Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object {
            $_.IPAddress -notlike "127.*" -and $_.IPAddress -notlike "169.254.*"
        } | ForEach-Object {
            [void]$ips.Add($_.IPAddress)
        }
    } catch {
        [void]$ips.Add("Unknown")
    }
    if ($ips.Count -eq 0) { [void]$ips.Add("Unknown") }
    return ,$ips
}

function Test-PortInUse {
    param([int]$Port)
    try {
        $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Any, $Port)
        $listener.Start()
        $listener.Stop()
        return $false
    } catch {
        return $true
    }
}

function Enable-UPnP {
    param([int]$Port)
    Write-Host ""
    Write-Host "[UPnP Port Forwarding]" -ForegroundColor Cyan
    try {
        $natupnp = New-Object -ComObject HNetCfg.NATUPnP
        $mappings = $natupnp.StaticPortMappingCollection
        if ($null -eq $mappings) {
            Write-Host "[Error] UPnP not available on this network" -ForegroundColor Red
            Write-Host "Your router may not support UPnP or it is disabled." -ForegroundColor Yellow
            return $false
        }
        $localIP = (Get-LocalIPs)[0]
        Write-Host "Adding port mapping: $Port -> ${localIP}:$Port"
        try { $mappings.Remove($Port, "TCP") } catch {}
        $mappings.Add($Port, "TCP", $Port, $localIP, $true, "SFO Game")
        Write-Host "[Success] UPnP port $Port forwarded to $localIP" -ForegroundColor Green
        return $true
    } catch {
        Write-Host "[Error] UPnP failed: $_" -ForegroundColor Red
        return $false
    }
}

function Run-Diagnostics {
    Write-Host ""
    Write-Host "[DIAGNOSTICS]" -ForegroundColor Cyan
    Write-Host "==========================================================="

    Write-Host ""
    Write-Host "1. Local IP Addresses:" -ForegroundColor Yellow
    Get-LocalIPs | ForEach-Object { Write-Host "   $_" }

    Write-Host ""
    Write-Host "2. Port 1626 Status:" -ForegroundColor Yellow
    if (Test-PortInUse 1626) {
        Write-Host "   IN USE (game or another app is using it)" -ForegroundColor Green
        try {
            $conn = Get-NetTCPConnection -LocalPort 1626 -ErrorAction SilentlyContinue
            if ($conn) {
                $proc = Get-Process -Id $conn.OwningProcess -ErrorAction SilentlyContinue
                if ($proc) {
                    Write-Host "   Process: $($proc.ProcessName) (PID: $($conn.OwningProcess))"
                }
            }
        } catch {}
    } else {
        Write-Host "   AVAILABLE (game not running or not in server mode)" -ForegroundColor Yellow
    }

    Write-Host ""
    Write-Host "3. Firewall Rules:" -ForegroundColor Yellow
    try {
        $rules = Get-NetFirewallRule -DisplayName "*SFO*" -ErrorAction SilentlyContinue
        if ($rules) {
            $rules | ForEach-Object { Write-Host "   $($_.DisplayName) - Enabled: $($_.Enabled)" }
        } else {
            Write-Host "   No SFO-specific rules found"
        }
    } catch {
        Write-Host "   Could not check firewall rules"
    }

    Write-Host ""
    Write-Host "4. UPnP Status:" -ForegroundColor Yellow
    try {
        $natupnp = New-Object -ComObject HNetCfg.NATUPnP
        $mappings = $natupnp.StaticPortMappingCollection
        if ($null -eq $mappings) {
            Write-Host "   UPnP not available or disabled on router" -ForegroundColor Red
        } else {
            Write-Host "   UPnP available" -ForegroundColor Green
            $found = $false
            foreach ($m in $mappings) {
                if ($m.ExternalPort -eq 1626) {
                    Write-Host "   Port 1626 mapped to: $($m.InternalClient)" -ForegroundColor Green
                    $found = $true
                }
            }
            if (-not $found) {
                Write-Host "   Port 1626 not mapped via UPnP" -ForegroundColor Yellow
            }
        }
    } catch {
        Write-Host "   Could not check UPnP: $_" -ForegroundColor Red
    }

    Write-Host ""
    Write-Host "5. External IP:" -ForegroundColor Yellow
    try {
        $response = Invoke-WebRequest -Uri "https://api.ipify.org" -TimeoutSec 5 -UseBasicParsing
        Write-Host "   $($response.Content)" -ForegroundColor Green
    } catch {
        Write-Host "   Could not determine external IP" -ForegroundColor Red
    }

    Write-Host ""
    Write-Host "==========================================================="
}

function Show-HostInfo {
    param([int]$Port)
    Write-Host ""
    Write-Host "[HOST MODE - Port $Port]" -ForegroundColor Cyan
    $ips = Get-LocalIPs
    Write-Host ""
    Write-Host "Your IP addresses:" -ForegroundColor Yellow
    $ips | ForEach-Object { Write-Host "  $_" }

    if (Test-PortInUse $Port) {
        Write-Host ""
        Write-Host "[OK] Port $Port is in use - game may be running" -ForegroundColor Green
    } else {
        Write-Host ""
        Write-Host "[Warning] Port $Port not in use - start game and enable Server" -ForegroundColor Yellow
    }

    Write-Host ""
    Write-Host "==========================================================="
    Write-Host "  SHARE WITH YOUR FRIEND:"
    Write-Host "==========================================================="
    Write-Host "  IP:   $($ips[0])"
    Write-Host "  PORT: 1626"
    Write-Host "==========================================================="
    Write-Host ""
    Write-Host "Steps to host:" -ForegroundColor Green
    Write-Host "  1. Start Street Fighter Online"
    Write-Host "  2. Go to Network Settings"
    Write-Host "  3. Turn Server ON"
    Write-Host "  4. Tell your friend your IP address"
}

function Add-FirewallRule {
    param([int]$Port)
    Write-Host ""
    Write-Host "[Adding Firewall Rule]" -ForegroundColor Cyan
    try {
        $existing = Get-NetFirewallRule -DisplayName "SFO Game Port $Port" -ErrorAction SilentlyContinue
        if ($existing) {
            Write-Host "[Info] Firewall rule already exists" -ForegroundColor Yellow
        } else {
            New-NetFirewallRule -DisplayName "SFO Game Port $Port" -Direction Inbound -Protocol TCP -LocalPort $Port -Action Allow -Profile Any -Description "Street Fighter Online game port" | Out-Null
            Write-Host "[Success] Firewall rule added for port $Port" -ForegroundColor Green
        }
    } catch {
        Write-Host "[Error] Could not add firewall rule (run as Admin): $_" -ForegroundColor Red
    }
}

function Show-Menu {
    while ($true) {
        Clear-Host
        Write-Host "==========================================================="
        Write-Host "         SFO NETWORK HELPER (Built-in Tools)"
        Write-Host "==========================================================="
        $ips = Get-LocalIPs
        Write-Host "Your IP: $($ips[0])"
        Write-Host ""
        Write-Host "  1. HOST INFO        Show IPs and instructions"
        Write-Host "  2. DIAGNOSTICS      Check network status"
        Write-Host "  3. ENABLE UPnP      Auto port-forward"
        Write-Host "  4. ADD FIREWALL     Allow port 1626"
        Write-Host "  5. EXIT"
        Write-Host ""
        $choice = Read-Host "Select option (1-5)"

        switch ($choice) {
            "1" { Show-HostInfo -Port $LocalPort; Read-Host "Press Enter to continue..." }
            "2" { Run-Diagnostics; Read-Host "Press Enter to continue..." }
            "3" { Enable-UPnP -Port $LocalPort; Read-Host "Press Enter to continue..." }
            "4" { Add-FirewallRule -Port $LocalPort; Read-Host "Press Enter to continue..." }
            "5" { return }
            default { Write-Host "Invalid choice" }
        }
    }
}

# Entry point
switch ($Mode) {
    "menu" { Show-Menu }
    "host" { Show-HostInfo -Port $LocalPort }
    "diagnose" { Run-Diagnostics }
    "upnp" { Enable-UPnP -Port $LocalPort }
    default { Show-Menu }
}
