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
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/client/bridge"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/client/config"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/client/transport"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/server/auth"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/server/ratelimit"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/server/relay"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/internal/server/session"
)

const (
	version = "2.1.0"
	banner  = `
╔═══════════════════════════════════════════════╗
║       SFO Connectivity Helper v%s          ║
║   Tunnel your game through NAT/CGNAT easily   ║
╚═══════════════════════════════════════════════╝
`
)

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

	for {
		fmt.Printf(banner, version)
		fmt.Println(`
╔═══════════════════════════════════════════════╗
║                 MAIN MENU                     ║
╠═══════════════════════════════════════════════╣
║  1. Quick Play     (Auto host/join - EASY!)   ║
║  2. Run Server     (Host infrastructure)      ║
║  3. Advanced       (Manual host/join/diagnose)║
║  4. Exit                                      ║
╚═══════════════════════════════════════════════╝
`)
		fmt.Print("Enter choice (1-4): ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			runQuickPlay(reader)
		case "2":
			runInteractiveServer(reader)
		case "3":
			runAdvancedMenu(reader)
		case "4":
			fmt.Println("Goodbye!")
			return
		default:
			fmt.Println("Invalid choice. Press Enter to continue...")
			reader.ReadString('\n')
		}
	}
}

func runAdvancedMenu(reader *bufio.Reader) {
	for {
		fmt.Println(`
╔═══════════════════════════════════════════════╗
║              ADVANCED OPTIONS                 ║
╠═══════════════════════════════════════════════╣
║  1. Host a Game    (Manual setup)             ║
║  2. Join a Game    (Manual setup)             ║
║  3. Diagnostics    (Test connectivity)        ║
║  4. Back to Main Menu                         ║
╚═══════════════════════════════════════════════╝
`)
		fmt.Print("Enter choice (1-4): ")

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1":
			runInteractiveHost(reader)
		case "2":
			runInteractiveJoin(reader)
		case "3":
			runInteractiveDiagnose(reader)
		case "4":
			return
		default:
			fmt.Println("Invalid choice.")
		}
	}
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

	signalURL := fmt.Sprintf("http://%s:8080", server)
	relayAddr := fmt.Sprintf("%s:8443", server)

	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────")
	fmt.Println("  Enter JOIN CODE from your friend")
	fmt.Println("  OR press Enter to HOST (create a new code)")
	fmt.Println("─────────────────────────────────────────────────")
	fmt.Print("\nJoin code (or Enter to host): ")

	code, _ := reader.ReadString('\n')
	code = strings.TrimSpace(code)

	args := []string{"--signal", signalURL, "--relay", relayAddr, "--skip-wait"}

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

	fmt.Print("Signaling server URL [http://localhost:8080]: ")
	signalURL, _ := reader.ReadString('\n')
	signalURL = strings.TrimSpace(signalURL)
	if signalURL == "" {
		signalURL = "http://localhost:8080"
	}

	fmt.Print("Relay server address [localhost:8443]: ")
	relayAddr, _ := reader.ReadString('\n')
	relayAddr = strings.TrimSpace(relayAddr)
	if relayAddr == "" {
		relayAddr = "localhost:8443"
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

	fmt.Print("Signaling server URL [http://localhost:8080]: ")
	signalURL, _ := reader.ReadString('\n')
	signalURL = strings.TrimSpace(signalURL)
	if signalURL == "" {
		signalURL = "http://localhost:8080"
	}

	fmt.Print("Relay server address [localhost:8443]: ")
	relayAddr, _ := reader.ReadString('\n')
	relayAddr = strings.TrimSpace(relayAddr)
	if relayAddr == "" {
		relayAddr = "localhost:8443"
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

	fmt.Print("Signaling port [8080]: ")
	sigPort, _ := reader.ReadString('\n')
	sigPort = strings.TrimSpace(sigPort)
	if sigPort == "" {
		sigPort = "8080"
	}

	fmt.Print("Relay port [8443]: ")
	relayPort, _ := reader.ReadString('\n')
	relayPort = strings.TrimSpace(relayPort)
	if relayPort == "" {
		relayPort = "8443"
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

	fmt.Print("Signaling server URL [http://localhost:8080]: ")
	signalURL, _ := reader.ReadString('\n')
	signalURL = strings.TrimSpace(signalURL)
	if signalURL == "" {
		signalURL = "http://localhost:8080"
	}

	fmt.Print("Relay server address [localhost:8443]: ")
	relayAddr, _ := reader.ReadString('\n')
	relayAddr = strings.TrimSpace(relayAddr)
	if relayAddr == "" {
		relayAddr = "localhost:8443"
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
  sfo-helper host --signal http://myserver:8080 --relay myserver:8443

  # Join a game (player 2)
  sfo-helper join --code ABCD-EFGH-IJKL --signal http://myserver:8080 --relay myserver:8443

Use "sfo-helper <command> --help" for more information about a command.
`)
}

// ==================== SERVER COMMAND ====================

func runServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)

	signalingPort := fs.Int("signaling-port", 8080, "Signaling server port")
	relayPort := fs.Int("relay-port", 8443, "Relay server port")
	secret := fs.String("secret", "changeme-in-production", "Shared secret for token signing")
	sessionTTL := fs.Int("session-ttl", 15, "Session TTL in minutes")
	maxSessionHours := fs.Int("max-session", 4, "Max session duration in hours")

	fs.Parse(args)

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

		code := strings.ToUpper(strings.ReplaceAll(req.Code, "-", ""))
		if len(code) == 12 {
			code = fmt.Sprintf("%s-%s-%s", code[0:4], code[4:8], code[8:12])
		}

		sess, err := store.Join(code)
		if err != nil {
			http.Error(w, "Invalid or expired code", http.StatusNotFound)
			return
		}

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
	r := relay.NewRelay(validator, 30*time.Second, maxDuration)

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

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Printf("║  JOIN CODE: %-33s ║\n", sess.Code)
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Share this code with your friend.")
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
	fmt.Println("(They have 30 seconds to enter the code)")
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
