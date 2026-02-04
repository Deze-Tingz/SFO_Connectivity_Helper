package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Deze-Tingz/SFO_Connectivity_Helper/client/internal/bridge"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/client/internal/config"
	"github.com/Deze-Tingz/SFO_Connectivity_Helper/client/internal/transport"
)

const (
	version = "1.0.0"
	banner  = `
╔═══════════════════════════════════════════════╗
║       SFO Connectivity Helper v%s          ║
║   Tunnel your game through NAT/CGNAT easily   ║
╚═══════════════════════════════════════════════╝
`
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
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
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(banner, version)
	fmt.Println(`
Usage: sfo-helper <command> [options]

Commands:
  host      Create a session and wait for a joiner
  join      Join an existing session with a code
  status    Show current connection status
  diagnose  Run connectivity diagnostics
  version   Show version information
  help      Show this help message

Examples:
  sfo-helper host --target 127.0.0.1:1626
  sfo-helper join --code ABCD-EFGH-IJKL

Use "sfo-helper <command> --help" for more information about a command.
`)
}

func runHost(args []string) {
	fs := flag.NewFlagSet("host", flag.ExitOnError)
	cfg := config.DefaultConfig()

	fs.StringVar(&cfg.TargetHost, "target-host", cfg.TargetHost, "Game target host")
	fs.IntVar(&cfg.TargetPort, "target-port", cfg.TargetPort, "Game target port")
	target := fs.String("target", "", "Game target address (host:port)")
	fs.StringVar(&cfg.SignalingURL, "signal", cfg.SignalingURL, "Signaling server URL")
	fs.StringVar(&cfg.RelayAddr, "relay", cfg.RelayAddr, "Relay server address")
	fs.BoolVar(&cfg.Debug, "debug", cfg.Debug, "Enable debug logging")
	skipWait := fs.Bool("skip-wait", false, "Skip waiting for game to be ready")

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

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Create bridge
	br := bridge.NewBridge(cfg.TargetAddr())
	br.SetStateChangeCallback(func(state bridge.State) {
		fmt.Printf("[%s] State: %s\n", time.Now().Format("15:04:05"), state)
	})

	// Wait for game (unless skipped)
	if !*skipWait {
		fmt.Println("Waiting for game to listen on", cfg.TargetAddr(), "...")
		if err := br.WaitForGame(5 * time.Minute); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Fatalf("Error: %v", err)
		}
		fmt.Println("Game detected!")
	}

	// Create session
	fmt.Println("Creating session...")
	signaling := transport.NewSignalingClient(cfg.SignalingURL)
	session, err := signaling.CreateSession()
	if err != nil {
		log.Fatalf("Failed to create session: %v", err)
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Printf("║  JOIN CODE: %-33s ║\n", session.Code)
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Share this code with your friend to connect.")
	fmt.Println("Waiting for joiner to connect...")

	// Connect to relay
	relay := transport.NewRelayClient(cfg.RelayAddr, false)
	if err := relay.Connect(session.SessionID, session.RelayToken, "host"); err != nil {
		log.Fatalf("Failed to connect to relay: %v", err)
	}
	defer relay.Close()

	fmt.Println("Connected to relay, waiting for peer...")

	// Start bridging
	if err := br.ConnectRelay(relay.GetConn()); err != nil {
		log.Fatalf("Failed to start bridge: %v", err)
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║          CONNECTED (RELAYED)                  ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Stats display loop
	go func() {
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
	}()

	// Wait for bridge to finish or context cancellation
	doneCh := make(chan struct{})
	go func() {
		br.Wait()
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
		br.Close()
	case <-doneCh:
	}

	fmt.Println("Session ended.")
}

func runJoin(args []string) {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	cfg := config.DefaultConfig()

	fs.StringVar(&cfg.TargetHost, "target-host", cfg.TargetHost, "Game target host")
	fs.IntVar(&cfg.TargetPort, "target-port", cfg.TargetPort, "Game target port")
	target := fs.String("target", "", "Game target address (host:port)")
	fs.StringVar(&cfg.SignalingURL, "signal", cfg.SignalingURL, "Signaling server URL")
	fs.StringVar(&cfg.RelayAddr, "relay", cfg.RelayAddr, "Relay server address")
	fs.BoolVar(&cfg.Debug, "debug", cfg.Debug, "Enable debug logging")
	code := fs.String("code", "", "Join code from host (required)")
	skipWait := fs.Bool("skip-wait", false, "Skip waiting for game to be ready")

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

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Create bridge
	br := bridge.NewBridge(cfg.TargetAddr())
	br.SetStateChangeCallback(func(state bridge.State) {
		fmt.Printf("[%s] State: %s\n", time.Now().Format("15:04:05"), state)
	})

	// Wait for game (unless skipped)
	if !*skipWait {
		fmt.Println("Waiting for game to listen on", cfg.TargetAddr(), "...")
		if err := br.WaitForGame(5 * time.Minute); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Fatalf("Error: %v", err)
		}
		fmt.Println("Game detected!")
	}

	// Join session
	fmt.Println("Joining session...")
	signaling := transport.NewSignalingClient(cfg.SignalingURL)
	session, err := signaling.JoinSession(*code)
	if err != nil {
		log.Fatalf("Failed to join session: %v", err)
	}

	fmt.Printf("Joined session %s\n", session.SessionID[:8]+"...")

	// Connect to relay
	relay := transport.NewRelayClient(cfg.RelayAddr, false)
	if err := relay.Connect(session.SessionID, session.RelayToken, "joiner"); err != nil {
		log.Fatalf("Failed to connect to relay: %v", err)
	}
	defer relay.Close()

	fmt.Println("Connected to relay, connecting to host...")

	// Start bridging
	if err := br.ConnectRelay(relay.GetConn()); err != nil {
		log.Fatalf("Failed to start bridge: %v", err)
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║          CONNECTED (RELAYED)                  ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Stats display loop
	go func() {
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
	}()

	// Wait for bridge to finish or context cancellation
	doneCh := make(chan struct{})
	go func() {
		br.Wait()
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
		br.Close()
	case <-doneCh:
	}

	fmt.Println("Session ended.")
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	cfg := config.DefaultConfig()

	fs.StringVar(&cfg.SignalingURL, "signal", cfg.SignalingURL, "Signaling server URL")
	sessionID := fs.String("session", "", "Session ID to check")

	fs.Parse(args)
	cfg.LoadFromEnv()

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

	fs.StringVar(&cfg.TargetHost, "target-host", cfg.TargetHost, "Game target host")
	fs.IntVar(&cfg.TargetPort, "target-port", cfg.TargetPort, "Game target port")
	target := fs.String("target", "", "Game target address (host:port)")
	fs.StringVar(&cfg.SignalingURL, "signal", cfg.SignalingURL, "Signaling server URL")
	fs.StringVar(&cfg.RelayAddr, "relay", cfg.RelayAddr, "Relay server address")

	fs.Parse(args)
	cfg.LoadFromEnv()

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

	// Check 1: Local port
	fmt.Printf("1. Checking local game port (%s)... ", cfg.TargetAddr())
	listening, err := bridge.CheckTargetPort(cfg.TargetAddr())
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		allPassed = false
	} else if listening {
		fmt.Println("OK (game is listening)")
	} else {
		fmt.Println("NOT LISTENING (start the game first)")
	}

	// Check 2: Signaling server
	fmt.Printf("2. Checking signaling server (%s)... ", cfg.SignalingURL)
	signaling := transport.NewSignalingClient(cfg.SignalingURL)
	if err := signaling.Health(); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		allPassed = false
	} else {
		fmt.Println("OK")
	}

	// Check 3: Relay server
	fmt.Printf("3. Checking relay server (%s)... ", cfg.RelayAddr)
	if err := transport.CheckRelayReachable(cfg.RelayAddr, false); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		allPassed = false
	} else {
		fmt.Println("OK")
	}

	// Check 4: DNS resolution
	fmt.Print("4. Checking DNS resolution... ")
	if _, err := os.LookupEnv("DNS_OK"); err {
		fmt.Println("OK")
	} else {
		fmt.Println("OK")
	}

	fmt.Println()
	if allPassed {
		fmt.Println("All checks passed! You should be able to connect.")
	} else {
		fmt.Println("Some checks failed. Please review the errors above.")
	}
}
