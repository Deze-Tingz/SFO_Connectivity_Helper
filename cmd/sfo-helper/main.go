package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/client/bridge"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/client/config"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/client/transport"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/p2p"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/server/auth"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/server/ratelimit"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/server/relay"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/server/session"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/upnp"
)

const (
	version = "4.0.0"
	banner  = `
╔═══════════════════════════════════════════════╗
║       SFO Connectivity Helper v%s          ║
║   WebRTC/ICE P2P - CGNAT-Proof Networking     ║
╚═══════════════════════════════════════════════╝
`
	// Game paths
	gameExeName   = "Street Fighter Online.exe"
	configName    = "config.sfo"
	gameDirEnvVar = "SFO_GAME_DIR"
)

// ==================== GAME LAUNCHER FUNCTIONS ====================

// findGameDir locates the Street Fighter Online installation
func findGameDir() (string, error) {
	// Check environment variable first
	if dir := os.Getenv(gameDirEnvVar); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, gameExeName)); err == nil {
			return dir, nil
		}
	}

	// Check common locations
	locations := []string{
		filepath.Join(os.Getenv("APPDATA"), "Street Fighter Online"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Street Fighter Online"),
		filepath.Join(os.Getenv("PROGRAMFILES"), "Street Fighter Online"),
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Street Fighter Online"),
		"C:\\Street Fighter Online",
		".", // Current directory
	}

	for _, loc := range locations {
		if loc == "" {
			continue
		}
		gamePath := filepath.Join(loc, gameExeName)
		if _, err := os.Stat(gamePath); err == nil {
			return loc, nil
		}
	}

	return "", fmt.Errorf("game not found - set %s environment variable", gameDirEnvVar)
}

// GameConfig represents the config.sfo file
type GameConfig struct {
	Lines    []string
	FilePath string
}

// readGameConfig reads the game's config.sfo
func readGameConfig(gameDir string) (*GameConfig, error) {
	configPath := filepath.Join(gameDir, configName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	return &GameConfig{
		Lines:    lines,
		FilePath: configPath,
	}, nil
}

// backupConfig creates a backup of the original config
func (gc *GameConfig) backup() error {
	backupPath := gc.FilePath + ".backup"
	// Only backup if no backup exists (preserve original)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return os.WriteFile(backupPath, []byte(strings.Join(gc.Lines, "\n")), 0644)
	}
	return nil
}

// restoreConfig restores the original config from backup
func restoreGameConfig(gameDir string) error {
	configPath := filepath.Join(gameDir, configName)
	backupPath := configPath + ".backup"

	if data, err := os.ReadFile(backupPath); err == nil {
		return os.WriteFile(configPath, data, 0644)
	}
	return nil
}

// Note: Game uses native ports - 1626 TCP, 1627 UDP
// No port modification needed

// launchGame starts the game executable
func launchGame(gameDir string) (*exec.Cmd, error) {
	gamePath := filepath.Join(gameDir, gameExeName)
	cmd := exec.Command(gamePath)
	cmd.Dir = gameDir

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

// waitForGameExit waits for the game process to exit
func waitForGameExit(cmd *exec.Cmd) {
	cmd.Wait()
}

// isGameRunning checks if the game is already running
func isGameRunning() bool {
	// Simple check - try to find process by name
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq Street Fighter Online.exe", "/NH")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "Street Fighter Online")
}

// findGameListeningAddr finds which address the game is listening on for a given port
func findGameListeningAddr(port int) string {
	// Try common addresses first
	addrs := []string{
		fmt.Sprintf("127.0.0.1:%d", port),
		fmt.Sprintf("%s:%d", getLocalIP(), port),
	}

	// Also try all local interfaces
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		ifAddrs, _ := iface.Addrs()
		for _, addr := range ifAddrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && ip.To4() != nil {
				addrs = append(addrs, fmt.Sprintf("%s:%d", ip.String(), port))
			}
		}
	}

	// Check each address
	for _, addr := range addrs {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return addr
		}
	}

	// Fallback - use netstat to find it
	cmd := exec.Command("netstat", "-ano")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		portStr := fmt.Sprintf(":%d", port)
		for _, line := range lines {
			if strings.Contains(line, portStr) && strings.Contains(line, "LISTENING") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					return fields[1] // Returns IP:PORT
				}
			}
		}
	}

	return fmt.Sprintf("127.0.0.1:%d", port) // Default fallback
}

func main() {
	// If no arguments, run interactive mode
	if len(os.Args) < 2 {
		runInteractive()
		return
	}

	command := os.Args[1]

	switch command {
	case "server":
		runServer(os.Args[2:])
	case "host":
		runHost(os.Args[2:])
	case "join":
		runJoin(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	case "diagnose":
		runDiagnose(os.Args[2:])
	case "version":
		fmt.Printf("sfo-helper version %s\n", version)
	case "help", "-h", "--help":
		printUsage()
		waitForEnter()
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printUsage()
		waitForEnter()
	}
}

func runInteractive() {
	reader := bufio.NewReader(os.Stdin)

	// Try to find game directory on startup
	gameDir, err := findGameDir()
	if err != nil {
		fmt.Printf(banner, version)
		fmt.Println("WARNING: Could not find Street Fighter Online installation!")
		fmt.Println("         Game launcher features will be disabled.")
		fmt.Println()
		fmt.Printf("Set the %s environment variable to your game folder.\n", gameDirEnvVar)
		fmt.Println()
	} else {
		fmt.Printf("Game found: %s\n", gameDir)
	}

	for {
		fmt.Printf(banner, version)
		fmt.Println(`
╔═══════════════════════════════════════════════╗
║                 MAIN MENU                     ║
╠═══════════════════════════════════════════════╣
║  1. HOST (P2P)     WebRTC/ICE - CGNAT-proof   ║
║  2. JOIN (P2P)     WebRTC/ICE - CGNAT-proof   ║
║  3. HOST (Relay)   Classic relay mode         ║
║  4. JOIN (Relay)   Classic relay mode         ║
║  5. PLAY OFFLINE   Launch game only           ║
║  6. Advanced       Manual options             ║
║  7. Exit                                      ║
╚═══════════════════════════════════════════════╝
`)
		fmt.Print("Enter choice (1-7): ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			runP2PHost(reader, gameDir)
		case "2":
			runP2PJoin(reader, gameDir)
		case "3":
			runLauncherHost(reader, gameDir)
		case "4":
			runLauncherJoin(reader, gameDir)
		case "5":
			runLauncherOffline(reader, gameDir)
		case "6":
			runAdvancedMenu(reader)
		case "7":
			fmt.Println("Goodbye!")
			return
		default:
			fmt.Println("Invalid choice. Press Enter to continue...")
			reader.ReadString('\n')
		}
	}
}

// ==================== LAUNCHER FUNCTIONS ====================

// runLauncherHost - Host with automatic game launch
func runLauncherHost(reader *bufio.Reader, gameDir string) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║           HOST GAME (AUTOMATIC)               ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	if gameDir == "" {
		fmt.Println("ERROR: Game directory not found!")
		fmt.Println("Please set SFO_GAME_DIR environment variable.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	if isGameRunning() {
		fmt.Println("ERROR: Game is already running!")
		fmt.Println("Please close it first.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	// Check if ports are available
	if checkPortInUse(1628) || checkPortInUse(1627) {
		fmt.Println("ERROR: Server ports (1627/1628) already in use!")
		fmt.Println("Close other instances first.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	// Open ports via UPnP (automatic port forwarding)
	fmt.Println("Opening ports via UPnP...")
	var upnpClient *upnp.UPnPClient
	upnpClient, err := upnp.NewUPnPClient()
	if err != nil {
		fmt.Printf("  UPnP not available: %v\n", err)
		fmt.Println("  (Ports may need manual forwarding on router)")
	} else {
		if err := upnpClient.OpenSFOPorts(1626); err != nil {
			fmt.Printf("  Warning: %v\n", err)
		} else {
			fmt.Println("  Ports 1626-1628 opened on router!")
		}
		// Get external IP
		if extIP, err := upnpClient.GetExternalIP(); err == nil {
			fmt.Printf("  External IP: %s\n", extIP)
		}
		// Cleanup UPnP ports when done
		defer func() {
			fmt.Println("Closing UPnP ports...")
			upnpClient.CloseSFOPorts(1626)
		}()
	}
	fmt.Println()

	fmt.Println("Starting server...")

	// Create context for cleanup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	store := session.NewStore(15 * time.Minute)
	secret := fmt.Sprintf("auto-%d", time.Now().UnixNano())
	signer := auth.NewSigner(secret)
	limiter := ratelimit.NewMultiLimiter()
	go runSignalingServer(ctx, 1628, store, signer, limiter, 15*time.Minute)
	go runRelayServer(ctx, 1627, signer, 4*time.Hour)
	time.Sleep(500 * time.Millisecond)

	fmt.Println("Server started!")
	fmt.Println()

	// Create session
	fmt.Println("Creating session...")
	signaling := transport.NewSignalingClient("http://localhost:1628")
	sess, err := signaling.CreateSession()
	if err != nil {
		fmt.Printf("ERROR: Failed to create session: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	localIP := getLocalIP()

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║         SHARE THIS WITH YOUR FRIEND:          ║")
	fmt.Println("╠═══════════════════════════════════════════════╣")
	fmt.Printf("║  JOIN CODE: %-33s ║\n", sess.Code)
	fmt.Printf("║  SERVER IP: %-33s ║\n", localIP)
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Connect to relay as host
	fmt.Println("Connecting to relay...")
	relayClient := transport.NewRelayClient("localhost:1627", false)
	if err := relayClient.Connect(sess.SessionID, sess.RelayToken, "host"); err != nil {
		fmt.Printf("ERROR: Failed to connect to relay: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println("Relay connected! Launching game...")
	fmt.Println()

	// Launch the game
	gameCmd, err := launchGame(gameDir)
	if err != nil {
		fmt.Printf("ERROR: Failed to launch game: %v\n", err)
		relayClient.Close()
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║  GAME LAUNCHED!                               ║")
	fmt.Println("║                                               ║")
	fmt.Println("║  In-game:                                     ║")
	fmt.Println("║  1. Go to Network Settings                    ║")
	fmt.Println("║  2. Turn Server ON                            ║")
	fmt.Println("║  3. Wait for friend to connect                ║")
	fmt.Println("║                                               ║")
	fmt.Println("║  Keep this window open while playing!         ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("JOIN CODE: %s | SERVER IP: %s\n", sess.Code, localIP)
	fmt.Println()

	// Wait for game to start listening (user must turn Server ON in-game)
	fmt.Println("[Waiting] For game server to start...")
	fmt.Println("          (Turn Server ON in Network Settings)")
	var gameAddr string
	for {
		gameAddr = findGameListeningAddr(1626)
		if gameAddr != "" {
			conn, err := net.DialTimeout("tcp", gameAddr, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				fmt.Printf("[Ready] Game server found at %s\n", gameAddr)
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	// Connect bridge IMMEDIATELY - this is critical!
	// Bridge must be connected before joiner arrives so relay forwarding works
	fmt.Println("[Connecting] Bridge to game and relay...")
	activeBridge := bridge.NewBridge(gameAddr)
	if err := activeBridge.ConnectRelay(relayClient.GetConn()); err != nil {
		fmt.Printf("ERROR: Bridge connection failed: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║  READY! Waiting for friend to join...         ║")
	fmt.Println("╠═══════════════════════════════════════════════╣")
	fmt.Printf("║  JOIN CODE: %-33s ║\n", sess.Code)
	fmt.Printf("║  SERVER IP: %-33s ║\n", localIP)
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Bridge runs in background, wait for either game exit or bridge done
	bridgeDone := make(chan struct{})
	go func() {
		activeBridge.Wait()
		close(bridgeDone)
	}()

	// Wait for game to exit
	fmt.Println("Playing... (keep this window open)")
	waitForGameExit(gameCmd)

	fmt.Println("\nGame closed. Cleaning up...")
	activeBridge.Close()
	relayClient.Close()
	cancel()
	<-bridgeDone

	fmt.Println("Session ended.")
	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

// runLauncherJoin - Join with automatic game launch
func runLauncherJoin(reader *bufio.Reader, gameDir string) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║           JOIN GAME (AUTOMATIC)               ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	if gameDir == "" {
		fmt.Println("ERROR: Game directory not found!")
		fmt.Println("Please set SFO_GAME_DIR environment variable.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	if isGameRunning() {
		fmt.Println("ERROR: Game is already running!")
		fmt.Println("Please close it first.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println("Your friend should have given you:")
	fmt.Println("  1. A JOIN CODE (like SFO-X7K2)")
	fmt.Println("  2. Their SERVER IP (like 192.168.1.5)")
	fmt.Println()

	fmt.Print("Enter JOIN CODE: ")
	code, _ := reader.ReadString('\n')
	code = strings.TrimSpace(code)
	if code == "" {
		fmt.Println("No code entered.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Print("Enter SERVER IP: ")
	serverIP, _ := reader.ReadString('\n')
	serverIP = strings.TrimSpace(serverIP)
	if serverIP == "" {
		fmt.Println("No IP entered.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println()
	signalURL := fmt.Sprintf("http://%s:1628", serverIP)
	relayAddr := fmt.Sprintf("%s:1627", serverIP)

	fmt.Println("Connecting to host...")

	// Join session
	signaling := transport.NewSignalingClient(signalURL)
	sess, err := signaling.JoinSession(code)
	if err != nil {
		fmt.Printf("ERROR: Failed to join session: %v\n", err)
		fmt.Println("Check the code and IP address.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Printf("Session joined! (ID: %s)\n", sess.SessionID[:8])

	// Connect to relay
	fmt.Println("Connecting to relay...")
	relayClient := transport.NewRelayClient(relayAddr, false)
	if err := relayClient.Connect(sess.SessionID, sess.RelayToken, "joiner"); err != nil {
		fmt.Printf("ERROR: Failed to connect to relay: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println("Relay connected! Launching game...")
	fmt.Println()

	// Launch the game
	gameCmd, err := launchGame(gameDir)
	if err != nil {
		fmt.Printf("ERROR: Failed to launch game: %v\n", err)
		relayClient.Close()
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║  GAME LAUNCHED!                               ║")
	fmt.Println("║                                               ║")
	fmt.Println("║  In-game:                                     ║")
	fmt.Println("║  1. Go to Network Settings                    ║")
	fmt.Println("║  2. Search for the host's game                ║")
	fmt.Println("║  3. Join and play!                            ║")
	fmt.Println("║                                               ║")
	fmt.Println("║  Keep this window open while playing!         ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Wait for game to start listening
	fmt.Println("[Waiting] For game to start...")
	var gameAddr string
	for {
		gameAddr = findGameListeningAddr(1626)
		if gameAddr != "" {
			conn, err := net.DialTimeout("tcp", gameAddr, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				fmt.Printf("[Ready] Game found at %s\n", gameAddr)
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	// Connect bridge IMMEDIATELY
	fmt.Println("[Connecting] Bridge to game and relay...")
	activeBridge := bridge.NewBridge(gameAddr)
	if err := activeBridge.ConnectRelay(relayClient.GetConn()); err != nil {
		fmt.Printf("ERROR: Bridge connection failed: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║  CONNECTED! You can now search for host game  ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	bridgeDone := make(chan struct{})
	go func() {
		activeBridge.Wait()
		close(bridgeDone)
	}()

	// Wait for game to exit
	fmt.Println("Playing... (keep this window open)")
	waitForGameExit(gameCmd)

	fmt.Println("\nGame closed. Cleaning up...")
	activeBridge.Close()
	relayClient.Close()
	<-bridgeDone

	fmt.Println("Session ended.")
	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

// ==================== WebRTC P2P FUNCTIONS ====================

// runP2PHost - Host with WebRTC/ICE (CGNAT-proof)
func runP2PHost(reader *bufio.Reader, gameDir string) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║       HOST GAME (WebRTC P2P - CGNAT-PROOF)    ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	if gameDir == "" {
		fmt.Println("ERROR: Game directory not found!")
		fmt.Println("Please set SFO_GAME_DIR environment variable.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	if isGameRunning() {
		fmt.Println("ERROR: Game is already running!")
		fmt.Println("Please close it first.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	// Try UPnP first (helps when not behind CGNAT)
	fmt.Println("Attempting UPnP port forwarding...")
	var upnpClient *upnp.UPnPClient
	upnpClient, err := upnp.NewUPnPClient()
	if err != nil {
		fmt.Printf("  UPnP not available: %v\n", err)
		fmt.Println("  (WebRTC will use STUN for NAT traversal)")
	} else {
		if err := upnpClient.OpenSFOPorts(1626); err != nil {
			fmt.Printf("  UPnP partial: %v\n", err)
		} else {
			fmt.Println("  UPnP ports 1626-1628 opened!")
		}
		if extIP, err := upnpClient.GetExternalIP(); err == nil {
			fmt.Printf("  External IP: %s\n", extIP)
		}
		defer func() {
			fmt.Println("Closing UPnP ports...")
			upnpClient.CloseSFOPorts(1626)
		}()
	}
	fmt.Println()

	// Start local signaling server for WebRTC
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("Starting signaling server on port 1628...")
	go p2p.RunSignalServer(ctx, 1628)
	time.Sleep(500 * time.Millisecond)

	// Create signaling client
	signalClient := p2p.NewSignalingClient("http://localhost:1628")

	// Create session
	fmt.Println("Creating P2P session...")
	sessionID, joinCode, err := signalClient.CreateSession()
	if err != nil {
		fmt.Printf("ERROR: Failed to create session: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}
	_ = sessionID // Used internally

	localIP := getLocalIP()

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║         SHARE THIS WITH YOUR FRIEND:          ║")
	fmt.Println("╠═══════════════════════════════════════════════╣")
	fmt.Printf("║  JOIN CODE: %-33s ║\n", joinCode)
	fmt.Printf("║  SERVER IP: %-33s ║\n", localIP)
	fmt.Println("╠═══════════════════════════════════════════════╣")
	fmt.Println("║  WebRTC/ICE will try:                         ║")
	fmt.Println("║  1. Direct P2P (fastest)                      ║")
	fmt.Println("║  2. STUN hole-punch (NAT traversal)           ║")
	fmt.Println("║  3. TURN relay (fallback)                     ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Create WebRTC peer connection
	fmt.Println("Creating WebRTC peer connection...")
	peerConn, err := p2p.NewPeerConnection("", "", "") // Using free STUN servers
	if err != nil {
		fmt.Printf("ERROR: Failed to create peer connection: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}
	defer peerConn.Close()

	// Set callbacks
	peerConn.SetCallbacks(
		func() { fmt.Println("[WebRTC] Connected!") },
		func() { fmt.Println("[WebRTC] Disconnected") },
		func(err error) { fmt.Printf("[WebRTC] Error: %v\n", err) },
	)

	// Exchange signals (creates offer, waits for answer)
	fmt.Println("Waiting for joiner to connect...")
	fmt.Println("(They need to enter the code and your IP)")
	if err := signalClient.ExchangeSignals(peerConn); err != nil {
		fmt.Printf("ERROR: Signaling failed: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	// Wait for WebRTC connection
	fmt.Println("Establishing WebRTC connection...")
	if err := peerConn.WaitForConnection(2 * time.Minute); err != nil {
		fmt.Printf("ERROR: Connection timeout: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	connType := peerConn.GetConnectionType()
	fmt.Printf("\n[SUCCESS] Connected via: %s\n", connType)

	// Launch the game
	fmt.Println("Launching game...")
	gameCmd, err := launchGame(gameDir)
	if err != nil {
		fmt.Printf("ERROR: Failed to launch game: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Printf("║  CONNECTED (%s)                       ║\n", strings.ToUpper(connType))
	fmt.Println("║                                               ║")
	fmt.Println("║  In-game:                                     ║")
	fmt.Println("║  1. Go to Network Settings                    ║")
	fmt.Println("║  2. Turn Server ON                            ║")
	fmt.Println("║  3. Wait for friend in lobby                  ║")
	fmt.Println("║                                               ║")
	fmt.Println("║  Keep this window open while playing!         ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Bridge WebRTC to game
	bridgeDone := make(chan struct{})
	go func() {
		defer close(bridgeDone)
		// Wait for game to be ready
		time.Sleep(3 * time.Second)
		for {
			gameAddr := findGameListeningAddr(1626)
			if gameAddr != "" {
				conn, err := net.DialTimeout("tcp", gameAddr, 500*time.Millisecond)
				if err == nil {
					conn.Close()
					fmt.Printf("[Bridge] Connecting WebRTC to game at %s...\n", gameAddr)
					if err := peerConn.BridgeToTCP(gameAddr); err != nil {
						fmt.Printf("[Bridge] Error: %v\n", err)
					}
					return
				}
			}
			time.Sleep(1 * time.Second)
		}
	}()

	// Wait for game to exit
	waitForGameExit(gameCmd)

	fmt.Println("\nGame closed. Cleaning up...")
	peerConn.Close()
	cancel()
	<-bridgeDone

	fmt.Println("Session ended.")
	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

// runP2PJoin - Join with WebRTC/ICE (CGNAT-proof)
func runP2PJoin(reader *bufio.Reader, gameDir string) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║       JOIN GAME (WebRTC P2P - CGNAT-PROOF)    ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	if gameDir == "" {
		fmt.Println("ERROR: Game directory not found!")
		fmt.Println("Please set SFO_GAME_DIR environment variable.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	if isGameRunning() {
		fmt.Println("ERROR: Game is already running!")
		fmt.Println("Please close it first.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println("Your friend should have given you:")
	fmt.Println("  1. A JOIN CODE (like SFO-X7K2)")
	fmt.Println("  2. Their SERVER IP (like 192.168.1.5)")
	fmt.Println()

	fmt.Print("Enter JOIN CODE: ")
	code, _ := reader.ReadString('\n')
	code = strings.TrimSpace(code)
	if code == "" {
		fmt.Println("No code entered.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Print("Enter SERVER IP: ")
	serverIP, _ := reader.ReadString('\n')
	serverIP = strings.TrimSpace(serverIP)
	if serverIP == "" {
		fmt.Println("No IP entered.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	signalURL := fmt.Sprintf("http://%s:1628", serverIP)

	fmt.Println()
	fmt.Println("Connecting to host's signaling server...")

	// Create signaling client
	signalClient := p2p.NewSignalingClient(signalURL)

	// Join session
	_, err := signalClient.JoinSession(code)
	if err != nil {
		fmt.Printf("ERROR: Failed to join session: %v\n", err)
		fmt.Println("Check the code and IP address.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println("Session joined! Creating WebRTC connection...")

	// Create WebRTC peer connection
	peerConn, err := p2p.NewPeerConnection("", "", "") // Using free STUN servers
	if err != nil {
		fmt.Printf("ERROR: Failed to create peer connection: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}
	defer peerConn.Close()

	// Set callbacks
	peerConn.SetCallbacks(
		func() { fmt.Println("[WebRTC] Connected!") },
		func() { fmt.Println("[WebRTC] Disconnected") },
		func(err error) { fmt.Printf("[WebRTC] Error: %v\n", err) },
	)

	// Exchange signals (waits for offer, sends answer)
	fmt.Println("Exchanging WebRTC signals with host...")
	if err := signalClient.ExchangeSignals(peerConn); err != nil {
		fmt.Printf("ERROR: Signaling failed: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	// Wait for WebRTC connection
	fmt.Println("Establishing WebRTC connection...")
	if err := peerConn.WaitForConnection(2 * time.Minute); err != nil {
		fmt.Printf("ERROR: Connection timeout: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	connType := peerConn.GetConnectionType()
	fmt.Printf("\n[SUCCESS] Connected via: %s\n", connType)

	// Launch the game
	fmt.Println("Launching game...")
	gameCmd, err := launchGame(gameDir)
	if err != nil {
		fmt.Printf("ERROR: Failed to launch game: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Printf("║  CONNECTED (%s)                       ║\n", strings.ToUpper(connType))
	fmt.Println("║                                               ║")
	fmt.Println("║  In-game:                                     ║")
	fmt.Println("║  1. Go to Network Settings                    ║")
	fmt.Println("║  2. Search for the host's game                ║")
	fmt.Println("║  3. Join and play!                            ║")
	fmt.Println("║                                               ║")
	fmt.Println("║  Keep this window open while playing!         ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Bridge WebRTC to game
	bridgeDone := make(chan struct{})
	go func() {
		defer close(bridgeDone)
		time.Sleep(3 * time.Second)
		for {
			gameAddr := findGameListeningAddr(1626)
			if gameAddr != "" {
				conn, err := net.DialTimeout("tcp", gameAddr, 500*time.Millisecond)
				if err == nil {
					conn.Close()
					fmt.Printf("[Bridge] Connecting WebRTC to game at %s...\n", gameAddr)
					if err := peerConn.BridgeToTCP(gameAddr); err != nil {
						fmt.Printf("[Bridge] Error: %v\n", err)
					}
					return
				}
			}
			time.Sleep(1 * time.Second)
		}
	}()

	// Wait for game to exit
	waitForGameExit(gameCmd)

	fmt.Println("\nGame closed. Cleaning up...")
	peerConn.Close()
	<-bridgeDone

	fmt.Println("Session ended.")
	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

// runLauncherOffline - Just launch the game normally
func runLauncherOffline(reader *bufio.Reader, gameDir string) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║           PLAY OFFLINE                        ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	if gameDir == "" {
		fmt.Println("ERROR: Game directory not found!")
		fmt.Println("Please set SFO_GAME_DIR environment variable.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	if isGameRunning() {
		fmt.Println("Game is already running!")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println("Launching game...")

	gameCmd, err := launchGame(gameDir)
	if err != nil {
		fmt.Printf("ERROR: Failed to launch game: %v\n", err)
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Println("Game launched! Waiting for exit...")
	waitForGameExit(gameCmd)

	fmt.Println("Game closed.")
	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

func runAdvancedMenu(reader *bufio.Reader) {
	for {
		fmt.Println(`
╔═══════════════════════════════════════════════╗
║              ADVANCED OPTIONS                 ║
╠═══════════════════════════════════════════════╣
║  1. Quick Play     (Connect to remote server) ║
║  2. Run Server     (Start your own server)    ║
║  3. Host (Manual)  (Custom settings)          ║
║  4. Join (Manual)  (Custom settings)          ║
║  5. Diagnostics    (Test connectivity)        ║
║  6. Back to Main Menu                         ║
╚═══════════════════════════════════════════════╝
`)
		fmt.Print("Enter choice (1-6): ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			runQuickPlay(reader)
		case "2":
			runInteractiveServer(reader)
		case "3":
			runInteractiveHost(reader)
		case "4":
			runInteractiveJoin(reader)
		case "5":
			runInteractiveDiagnose(reader)
		case "6":
			return
		default:
			fmt.Println("Invalid choice.")
		}
	}
}

// runOneClickHost - Starts server + host automatically. Simplest way to host!
func runOneClickHost(reader *bufio.Reader) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║           ONE-CLICK HOST                      ║")
	fmt.Println("║   Server + Host started automatically!        ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Check if ports are already in use
	sigInUse := checkPortInUse(1628)
	relayInUse := checkPortInUse(1627)

	if sigInUse || relayInUse {
		fmt.Println("Server ports are in use (another server running)")
		fmt.Println("Connecting to existing server...")
	} else {
		fmt.Println("Starting server...")
		// Start server in background goroutine
		go func() {
			ctx, _ := context.WithCancel(context.Background())
			store := session.NewStore(15 * time.Minute)
			secret := fmt.Sprintf("auto-%d", time.Now().UnixNano())
			signer := auth.NewSigner(secret)
			limiter := ratelimit.NewMultiLimiter()
			go runSignalingServer(ctx, 1628, store, signer, limiter, 15*time.Minute)
			go runRelayServer(ctx, 1627, signer, 4*time.Hour)
		}()
		time.Sleep(500 * time.Millisecond) // Give server time to start
		fmt.Println("Server started!")
	}

	fmt.Println()
	fmt.Println("Make sure Street Fighter Online is running with Server ON!")
	fmt.Println()

	// Run host with localhost defaults
	args := []string{
		"--signal", "http://localhost:1628",
		"--relay", "localhost:1627",
		"--target", "127.0.0.1:1626",
		"--skip-wait",
	}
	runHost(args)

	fmt.Println("\n══════════════════════════════════════════")
	fmt.Println("Session ended.")
	fmt.Println("══════════════════════════════════════════")
	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

// runOneClickJoin - Just enter code and go!
func runOneClickJoin(reader *bufio.Reader) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║           JOIN A GAME                         ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Make sure Street Fighter Online is running!")
	fmt.Println()
	fmt.Println("Your friend should have given you TWO things:")
	fmt.Println("  1. A JOIN CODE (like ABCD-EFGH-IJKL)")
	fmt.Println("  2. Their SERVER IP (like 192.168.1.5)")
	fmt.Println()

	fmt.Print("Enter JOIN CODE: ")
	code, _ := reader.ReadString('\n')
	code = strings.TrimSpace(code)

	if code == "" {
		fmt.Println("No code entered.")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	fmt.Print("Enter SERVER IP (from your friend): ")
	server, _ := reader.ReadString('\n')
	server = strings.TrimSpace(server)
	if server == "" {
		fmt.Println("You must enter the host's IP address!")
		fmt.Println("\nPress Enter to return to menu...")
		reader.ReadString('\n')
		return
	}

	signalURL := fmt.Sprintf("http://%s:1628", server)
	relayAddr := fmt.Sprintf("%s:1627", server)

	fmt.Println()
	fmt.Println("Connecting...")

	args := []string{
		"--code", code,
		"--signal", signalURL,
		"--relay", relayAddr,
		"--target", "127.0.0.1:1626",
		"--skip-wait",
	}
	runJoin(args)

	fmt.Println("\n══════════════════════════════════════════")
	fmt.Println("Session ended.")
	fmt.Println("══════════════════════════════════════════")
	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

func runQuickPlay(reader *bufio.Reader) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║              QUICK PLAY                       ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Make sure Street Fighter Online is running first!")
	fmt.Println()

	// Get server address (with default)
	fmt.Print("Server address [localhost]: ")
	server, _ := reader.ReadString('\n')
	server = strings.TrimSpace(server)
	if server == "" {
		server = "localhost"
	}

	signalURL := fmt.Sprintf("http://%s:1628", server)
	relayAddr := fmt.Sprintf("%s:1627", server)

	// Get game target address
	fmt.Print("Game target address [127.0.0.1:1626]: ")
	target, _ := reader.ReadString('\n')
	target = strings.TrimSpace(target)
	if target == "" {
		target = "127.0.0.1:1626"
	}

	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────")
	fmt.Println("  Enter JOIN CODE from your friend")
	fmt.Println("  OR press Enter to HOST (create a new code)")
	fmt.Println("─────────────────────────────────────────────────")
	fmt.Print("\nJoin code (or Enter to host): ")

	code, _ := reader.ReadString('\n')
	code = strings.TrimSpace(code)

	args := []string{"--signal", signalURL, "--relay", relayAddr, "--target", target, "--skip-wait"}

	if code == "" {
		// HOST MODE
		fmt.Println("\n>>> HOSTING - Creating session...")
		runHost(args)
	} else {
		// JOIN MODE
		fmt.Println("\n>>> JOINING - Connecting to host...")
		args = append(args, "--code", code)
		runJoin(args)
	}

	fmt.Println("\n══════════════════════════════════════════")
	fmt.Println("Session ended.")
	fmt.Println("══════════════════════════════════════════")
	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

func runInteractiveHost(reader *bufio.Reader) {
	fmt.Println("\n═══ HOST A GAME ═══")
	fmt.Println("IMPORTANT: Start Street Fighter Online FIRST before continuing!")
	fmt.Println()

	fmt.Print("Signaling server URL [http://localhost:1628]: ")
	signalURL, _ := reader.ReadString('\n')
	signalURL = strings.TrimSpace(signalURL)
	if signalURL == "" {
		signalURL = "http://localhost:1628"
	}

	fmt.Print("Relay server address [localhost:1627]: ")
	relayAddr, _ := reader.ReadString('\n')
	relayAddr = strings.TrimSpace(relayAddr)
	if relayAddr == "" {
		relayAddr = "localhost:1627"
	}

	fmt.Print("Game target address [127.0.0.1:1626]: ")
	target, _ := reader.ReadString('\n')
	target = strings.TrimSpace(target)
	if target == "" {
		target = "127.0.0.1:1626"
	}

	fmt.Print("Skip waiting for game? (y/N): ")
	skipWait, _ := reader.ReadString('\n')
	skipWait = strings.TrimSpace(strings.ToLower(skipWait))

	// Build args
	args := []string{"--signal", signalURL, "--relay", relayAddr, "--target", target}
	if skipWait == "y" || skipWait == "yes" {
		args = append(args, "--skip-wait")
	}

	fmt.Println("\nStarting host mode...")
	fmt.Println("Keep this window open! Press Ctrl+C to stop.\n")

	runHost(args)

	fmt.Println("\n══════════════════════════════════════════")
	fmt.Println("Session ended. Possible reasons:")
	fmt.Println("  - Your friend disconnected")
	fmt.Println("  - The game was closed")
	fmt.Println("  - Connection timed out (no joiner within 30 sec)")
	fmt.Println("  - Server was stopped")
	fmt.Println("══════════════════════════════════════════")
	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

func runInteractiveJoin(reader *bufio.Reader) {
	fmt.Println("\n═══ JOIN A GAME ═══")
	fmt.Println("IMPORTANT: Start Street Fighter Online FIRST before continuing!")
	fmt.Println()

	fmt.Print("Enter join code: ")
	code, _ := reader.ReadString('\n')
	code = strings.TrimSpace(code)
	if code == "" {
		fmt.Println("No code entered. Press Enter to return...")
		reader.ReadString('\n')
		return
	}

	fmt.Print("Signaling server URL [http://localhost:1628]: ")
	signalURL, _ := reader.ReadString('\n')
	signalURL = strings.TrimSpace(signalURL)
	if signalURL == "" {
		signalURL = "http://localhost:1628"
	}

	fmt.Print("Relay server address [localhost:1627]: ")
	relayAddr, _ := reader.ReadString('\n')
	relayAddr = strings.TrimSpace(relayAddr)
	if relayAddr == "" {
		relayAddr = "localhost:1627"
	}

	fmt.Print("Game target address [127.0.0.1:1626]: ")
	target, _ := reader.ReadString('\n')
	target = strings.TrimSpace(target)
	if target == "" {
		target = "127.0.0.1:1626"
	}

	fmt.Print("Skip waiting for game? (y/N): ")
	skipWait, _ := reader.ReadString('\n')
	skipWait = strings.TrimSpace(strings.ToLower(skipWait))

	args := []string{"--code", code, "--signal", signalURL, "--relay", relayAddr, "--target", target}
	if skipWait == "y" || skipWait == "yes" {
		args = append(args, "--skip-wait")
	}

	fmt.Println("\nStarting join mode...")
	fmt.Println("Keep this window open! Press Ctrl+C to stop.\n")

	runJoin(args)

	fmt.Println("\n══════════════════════════════════════════")
	fmt.Println("Session ended. Possible reasons:")
	fmt.Println("  - Host disconnected")
	fmt.Println("  - The game was closed")
	fmt.Println("  - Connection was lost")
	fmt.Println("  - Server was stopped")
	fmt.Println("══════════════════════════════════════════")
	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

func runInteractiveServer(reader *bufio.Reader) {
	fmt.Println("\n═══ RUN SERVER ═══")
	fmt.Println("This runs both signaling and relay servers.")

	fmt.Print("Secret key for tokens [auto-generate]: ")
	secret, _ := reader.ReadString('\n')
	secret = strings.TrimSpace(secret)
	if secret == "" {
		secret = fmt.Sprintf("auto-%d", time.Now().UnixNano())
		fmt.Printf("Generated secret: %s\n", secret)
	}

	fmt.Print("Signaling port [1628]: ")
	sigPort, _ := reader.ReadString('\n')
	sigPort = strings.TrimSpace(sigPort)
	if sigPort == "" {
		sigPort = "1628"
	}

	fmt.Print("Relay port [1627]: ")
	relayPort, _ := reader.ReadString('\n')
	relayPort = strings.TrimSpace(relayPort)
	if relayPort == "" {
		relayPort = "1627"
	}

	args := []string{"--secret", secret, "--signaling-port", sigPort, "--relay-port", relayPort}

	fmt.Println("\nStarting servers...")
	fmt.Println("Press Ctrl+C to stop.\n")

	runServer(args)

	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

func runInteractiveDiagnose(reader *bufio.Reader) {
	fmt.Println("\n═══ DIAGNOSTICS ═══")

	fmt.Print("Signaling server URL [http://localhost:1628]: ")
	signalURL, _ := reader.ReadString('\n')
	signalURL = strings.TrimSpace(signalURL)
	if signalURL == "" {
		signalURL = "http://localhost:1628"
	}

	fmt.Print("Relay server address [localhost:1627]: ")
	relayAddr, _ := reader.ReadString('\n')
	relayAddr = strings.TrimSpace(relayAddr)
	if relayAddr == "" {
		relayAddr = "localhost:1627"
	}

	fmt.Print("Game target address [127.0.0.1:1626]: ")
	target, _ := reader.ReadString('\n')
	target = strings.TrimSpace(target)
	if target == "" {
		target = "127.0.0.1:1626"
	}

	args := []string{"--signal", signalURL, "--relay", relayAddr, "--target", target}

	fmt.Println()
	runDiagnose(args)

	fmt.Println("\nPress Enter to return to menu...")
	reader.ReadString('\n')
}

func waitForEnter() {
	fmt.Println("\nPress Enter to exit...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func printUsage() {
	fmt.Printf(banner, version)
	fmt.Println(`
Usage: sfo-helper <command> [options]

Commands:
  server    Run signaling + relay servers (for hosting infrastructure)
  host      Create a session and wait for a joiner (player)
  join      Join an existing session with a code (player)
  status    Show current connection status
  diagnose  Run connectivity diagnostics
  version   Show version information
  help      Show this help message

Examples:
  # Run the server (on your VPS or home server)
  sfo-helper server --secret mysecretkey

  # Host a game (player 1)
  sfo-helper host --signal http://myserver:1628 --relay myserver:1627

  # Join a game (player 2)
  sfo-helper join --code ABCD-EFGH-IJKL --signal http://myserver:1628 --relay myserver:1627

Use "sfo-helper <command> --help" for more information about a command.
`)
}

// ==================== SERVER COMMAND ====================

func checkPortInUse(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	ln.Close()
	return false
}

// getLocalIP finds the preferred outbound IP address of this machine
func getLocalIP() string {
	// Method 1: Connect to external address to find outbound IP
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP.String()
	}

	// Method 2: Scan network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return "unknown"
	}

	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Skip loopback and IPv6
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}

			return ip.String()
		}
	}

	return "unknown"
}

func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)

	signalingPort := fs.Int("signaling-port", 1628, "Signaling server port")
	relayPort := fs.Int("relay-port", 1627, "Relay server port")
	secret := fs.String("secret", "changeme-in-production", "Shared secret for token signing")
	sessionTTL := fs.Int("session-ttl", 15, "Session TTL in minutes")
	maxSessionHours := fs.Int("max-session", 4, "Max session duration in hours")

	fs.Parse(args)

	// Check if ports are already in use
	sigInUse := checkPortInUse(*signalingPort)
	relayInUse := checkPortInUse(*relayPort)

	if sigInUse || relayInUse {
		fmt.Println()
		fmt.Println("╔═══════════════════════════════════════════════╗")
		fmt.Println("║  WARNING: Ports already in use!               ║")
		fmt.Println("╚═══════════════════════════════════════════════╝")
		if sigInUse {
			fmt.Printf("  Port %d (signaling) is in use\n", *signalingPort)
		}
		if relayInUse {
			fmt.Printf("  Port %d (relay) is in use\n", *relayPort)
		}
		fmt.Println()
		fmt.Println("Another server instance may be running.")
		fmt.Println("Close it first, or use different ports with:")
		fmt.Println("  --signaling-port XXXX --relay-port XXXX")
		fmt.Println()
		return
	}

	if *secret == "changeme-in-production" {
		log.Println("WARNING: Using default secret. Use --secret in production!")
	}

	fmt.Printf(banner, version)
	fmt.Println("Mode: SERVER")
	fmt.Printf("Signaling port: %d\n", *signalingPort)
	fmt.Printf("Relay port: %d\n", *relayPort)
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down servers...")
		cancel()
	}()

	// Create shared components
	store := session.NewStore(time.Duration(*sessionTTL) * time.Minute)
	signer := auth.NewSigner(*secret)
	limiter := ratelimit.NewMultiLimiter()

	// Start signaling server
	go runSignalingServer(ctx, *signalingPort, store, signer, limiter, time.Duration(*sessionTTL)*time.Minute)

	// Start relay server
	go runRelayServer(ctx, *relayPort, signer, time.Duration(*maxSessionHours)*time.Hour)

	<-ctx.Done()
	fmt.Println("Servers stopped.")
}

func runSignalingServer(ctx context.Context, port int, store *session.Store, signer *auth.Signer, limiter *ratelimit.MultiLimiter, tokenTTL time.Duration) {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/session/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ip := getClientIP(r)
		if !limiter.AllowCreate(ip) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		sess, err := store.Create()
		if err != nil {
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		// Mark host as connected
		store.SetHostConnected(sess.ID, true)

		relayToken, _ := signer.CreateRelayToken(sess.ID, "host", tokenTTL)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessionId":  sess.ID,
			"code":       sess.Code,
			"hostToken":  sess.HostToken,
			"relayToken": relayToken,
			"expiresAt":  sess.ExpiresAt.Unix(),
		})
		log.Printf("Created session %s with code %s", sess.ID[:8], sess.Code)
	})

	mux.HandleFunc("/session/join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ip := getClientIP(r)
		if !limiter.AllowJoin(ip) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		var req struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		code := strings.ToUpper(strings.TrimSpace(req.Code))
		// Normalize code: accept "SFO-XXXX", "SFOXXXX", or just "XXXX"
		code = strings.ReplaceAll(code, "-", "")
		if strings.HasPrefix(code, "SFO") {
			code = code[3:] // Remove SFO prefix
		}
		if len(code) == 4 {
			code = fmt.Sprintf("SFO-%s", code)
		}

		sess, err := store.Join(code)
		if err != nil {
			http.Error(w, "Invalid or expired code", http.StatusNotFound)
			return
		}

		// Mark joiner as connected so host knows to connect bridge
		store.SetJoinConnected(sess.ID, true)

		relayToken, _ := signer.CreateRelayToken(sess.ID, "joiner", tokenTTL)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessionId":     sess.ID,
			"joinToken":     sess.JoinToken,
			"relayToken":    relayToken,
			"hostConnected": sess.HostConnected,
		})
		log.Printf("Joiner connected to session %s", sess.ID[:8])
	})

	mux.HandleFunc("/session/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/session/"), "/")
		if len(parts) < 2 || parts[1] != "status" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		sess, ok := store.GetByID(parts[0])
		if !ok {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessionId":     sess.ID,
			"hostConnected": sess.HostConnected,
			"joinConnected": sess.JoinConnected,
			"expiresAt":     sess.ExpiresAt.Unix(),
		})
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		mux.ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	log.Printf("Signaling server listening on :%d", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("Signaling server error: %v", err)
	}
}

func runRelayServer(ctx context.Context, port int, signer *auth.Signer, maxDuration time.Duration) {
	validator := &tokenValidator{signer: signer}
	r := relay.NewRelay(validator, 24*time.Hour, maxDuration) // Long timeout - wait for joiner indefinitely

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Failed to start relay listener: %v", err)
	}

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	log.Printf("Relay server listening on :%d", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("Accept error: %v", err)
				continue
			}
		}
		go r.HandleConnection(conn)
	}
}

type tokenValidator struct {
	signer *auth.Signer
}

func (v *tokenValidator) Validate(token string) (sessionID, role string, err error) {
	claims, err := v.signer.Verify(token)
	if err != nil {
		return "", "", err
	}
	return claims.SessionID, claims.Role, nil
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

// ==================== CLIENT COMMANDS ====================

func runHost(args []string) {
	fs := flag.NewFlagSet("host", flag.ExitOnError)
	cfg := config.DefaultConfig()

	target := fs.String("target", "", "Game target address (host:port)")
	fs.StringVar(&cfg.SignalingURL, "signal", cfg.SignalingURL, "Signaling server URL")
	fs.StringVar(&cfg.RelayAddr, "relay", cfg.RelayAddr, "Relay server address")
	fs.BoolVar(&cfg.Debug, "debug", cfg.Debug, "Enable debug logging")
	skipWait := fs.Bool("skip-wait", false, "Skip waiting for game")

	fs.Parse(args)
	cfg.LoadFromEnv()

	if *target != "" {
		parts := strings.Split(*target, ":")
		if len(parts) == 2 {
			cfg.TargetHost = parts[0]
			fmt.Sscanf(parts[1], "%d", &cfg.TargetPort)
		}
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	fmt.Printf(banner, version)
	fmt.Println("Mode: HOST")
	fmt.Printf("Target: %s\n", cfg.TargetAddr())
	fmt.Printf("Signaling: %s\n", cfg.SignalingURL)
	fmt.Printf("Relay: %s\n\n", cfg.RelayAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	br := bridge.NewBridge(cfg.TargetAddr())
	br.SetStateChangeCallback(func(state bridge.State) {
		fmt.Printf("[%s] State: %s\n", time.Now().Format("15:04:05"), state)
	})

	if !*skipWait {
		fmt.Println("Waiting for game on", cfg.TargetAddr(), "...")
		if err := br.WaitForGame(5 * time.Minute); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Fatalf("Error: %v", err)
		}
		fmt.Println("Game detected!")
	}

	fmt.Println("Creating session...")
	signaling := transport.NewSignalingClient(cfg.SignalingURL)
	sess, err := signaling.CreateSession()
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	localIP := getLocalIP()

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║         SHARE THIS WITH YOUR FRIEND:          ║")
	fmt.Println("╠═══════════════════════════════════════════════╣")
	fmt.Printf("║  JOIN CODE: %-33s ║\n", sess.Code)
	fmt.Printf("║  SERVER IP: %-33s ║\n", localIP)
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Your friend needs BOTH the code AND the IP!")
	fmt.Println("Waiting for joiner...")

	fmt.Println("Connecting to relay server...")
	relayClient := transport.NewRelayClient(cfg.RelayAddr, false)
	if err := relayClient.Connect(sess.SessionID, sess.RelayToken, "host"); err != nil {
		fmt.Printf("\nERROR: Failed to connect to relay: %v\n", err)
		fmt.Println("Make sure the server is running and the address is correct.")
		return
	}
	defer relayClient.Close()

	fmt.Println("Connected to relay! Waiting for your friend to join...")
	fmt.Println("(They have 2 minutes to enter the code)")
	fmt.Println()

	if err := br.ConnectRelay(relayClient.GetConn()); err != nil {
		fmt.Printf("\nERROR: Failed to connect to game: %v\n", err)
		fmt.Println("Make sure Street Fighter Online is running!")
		return
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║    SUCCESS! CONNECTED (RELAYED)               ║")
	fmt.Println("║    You can now play! Keep this window open.   ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	go statsLoop(ctx, br)

	doneCh := make(chan struct{})
	go func() {
		br.Wait()
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
		fmt.Println("\nYou stopped the session.")
		br.Close()
	case <-doneCh:
		fmt.Println("\nConnection ended.")
	}
}

func runJoin(args []string) {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	cfg := config.DefaultConfig()

	target := fs.String("target", "", "Game target address (host:port)")
	fs.StringVar(&cfg.SignalingURL, "signal", cfg.SignalingURL, "Signaling server URL")
	fs.StringVar(&cfg.RelayAddr, "relay", cfg.RelayAddr, "Relay server address")
	fs.BoolVar(&cfg.Debug, "debug", cfg.Debug, "Enable debug logging")
	code := fs.String("code", "", "Join code from host (required)")
	skipWait := fs.Bool("skip-wait", false, "Skip waiting for game")

	fs.Parse(args)
	cfg.LoadFromEnv()

	if *target != "" {
		parts := strings.Split(*target, ":")
		if len(parts) == 2 {
			cfg.TargetHost = parts[0]
			fmt.Sscanf(parts[1], "%d", &cfg.TargetPort)
		}
	}

	if *code == "" {
		fmt.Println("Error: --code is required")
		fs.Usage()
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	fmt.Printf(banner, version)
	fmt.Println("Mode: JOIN")
	fmt.Printf("Target: %s\n", cfg.TargetAddr())
	fmt.Printf("Code: %s\n\n", *code)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	br := bridge.NewBridge(cfg.TargetAddr())
	br.SetStateChangeCallback(func(state bridge.State) {
		fmt.Printf("[%s] State: %s\n", time.Now().Format("15:04:05"), state)
	})

	if !*skipWait {
		fmt.Println("Waiting for game on", cfg.TargetAddr(), "...")
		if err := br.WaitForGame(5 * time.Minute); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Fatalf("Error: %v", err)
		}
		fmt.Println("Game detected!")
	}

	fmt.Println("Joining session...")
	signaling := transport.NewSignalingClient(cfg.SignalingURL)
	sess, err := signaling.JoinSession(*code)
	if err != nil {
		log.Fatalf("Failed to join session: %v", err)
	}

	fmt.Printf("Joined session %s...\n", sess.SessionID[:8])

	fmt.Println("Connecting to relay server...")
	relayClient := transport.NewRelayClient(cfg.RelayAddr, false)
	if err := relayClient.Connect(sess.SessionID, sess.RelayToken, "joiner"); err != nil {
		fmt.Printf("\nERROR: Failed to connect to relay: %v\n", err)
		fmt.Println("Make sure the server is running and the address is correct.")
		return
	}
	defer relayClient.Close()

	fmt.Println("Connected to relay! Connecting to host...")

	if err := br.ConnectRelay(relayClient.GetConn()); err != nil {
		fmt.Printf("\nERROR: Failed to connect to game: %v\n", err)
		fmt.Println("Make sure Street Fighter Online is running!")
		return
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║    SUCCESS! CONNECTED (RELAYED)               ║")
	fmt.Println("║    You can now play! Keep this window open.   ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	go statsLoop(ctx, br)

	doneCh := make(chan struct{})
	go func() {
		br.Wait()
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
		fmt.Println("\nYou stopped the session.")
		br.Close()
	case <-doneCh:
		fmt.Println("\nConnection ended.")
	}
}

func statsLoop(ctx context.Context, br *bridge.Bridge) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := br.GetStats()
			fmt.Printf("[Stats] In: %d bytes | Out: %d bytes | Uptime: %s\n",
				stats.BytesIn.Load(),
				stats.BytesOut.Load(),
				time.Since(stats.StartTime).Round(time.Second))
		}
	}
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	cfg := config.DefaultConfig()

	fs.StringVar(&cfg.SignalingURL, "signal", cfg.SignalingURL, "Signaling server URL")
	sessionID := fs.String("session", "", "Session ID to check")

	fs.Parse(args)

	if *sessionID == "" {
		fmt.Println("Error: --session is required")
		fs.Usage()
		os.Exit(1)
	}

	signaling := transport.NewSignalingClient(cfg.SignalingURL)
	status, err := signaling.GetSessionStatus(*sessionID)
	if err != nil {
		log.Fatalf("Failed to get status: %v", err)
	}

	fmt.Println("Session Status:")
	fmt.Printf("  Session ID: %s\n", status.SessionID)
	fmt.Printf("  Host Connected: %v\n", status.HostConnected)
	fmt.Printf("  Join Connected: %v\n", status.JoinConnected)
	fmt.Printf("  Expires At: %s\n", time.Unix(status.ExpiresAt, 0).Format(time.RFC3339))
}

func runDiagnose(args []string) {
	fs := flag.NewFlagSet("diagnose", flag.ExitOnError)
	cfg := config.DefaultConfig()

	target := fs.String("target", "", "Game target address (host:port)")
	fs.StringVar(&cfg.SignalingURL, "signal", cfg.SignalingURL, "Signaling server URL")
	fs.StringVar(&cfg.RelayAddr, "relay", cfg.RelayAddr, "Relay server address")

	fs.Parse(args)

	if *target != "" {
		parts := strings.Split(*target, ":")
		if len(parts) == 2 {
			cfg.TargetHost = parts[0]
			fmt.Sscanf(parts[1], "%d", &cfg.TargetPort)
		}
	}

	fmt.Printf(banner, version)
	fmt.Println("Running diagnostics...\n")

	allPassed := true

	fmt.Printf("1. Checking local game port (%s)... ", cfg.TargetAddr())
	listening, _ := bridge.CheckTargetPort(cfg.TargetAddr())
	if listening {
		fmt.Println("OK (game is listening)")
	} else {
		fmt.Println("NOT LISTENING (start the game first)")
	}

	fmt.Printf("2. Checking signaling server (%s)... ", cfg.SignalingURL)
	signaling := transport.NewSignalingClient(cfg.SignalingURL)
	if err := signaling.Health(); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		allPassed = false
	} else {
		fmt.Println("OK")
	}

	fmt.Printf("3. Checking relay server (%s)... ", cfg.RelayAddr)
	if err := checkRelay(cfg.RelayAddr); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		allPassed = false
	} else {
		fmt.Println("OK")
	}

	fmt.Println()
	if allPassed {
		fmt.Println("All server checks passed! You should be able to connect.")
	} else {
		fmt.Println("Some checks failed. Please review the errors above.")
	}
}

func checkRelay(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		// Try with TLS
		conn, err = tls.DialWithDialer(
			&net.Dialer{Timeout: 5 * time.Second},
			"tcp", addr,
			&tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true},
		)
		if err != nil {
			return fmt.Errorf("unreachable: %w", err)
		}
	}
	conn.Close()
	return nil
}
