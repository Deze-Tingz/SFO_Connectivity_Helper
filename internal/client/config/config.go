package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

// Config holds the client configuration
type Config struct {
	TargetHost      string
	TargetPort      int
	SignalingURL    string
	RelayAddr       string
	AlwaysRelay     bool
	Debug           bool
	LocalListenPort int
	BindInterface   string
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		TargetHost:   "127.0.0.1",
		TargetPort:   1626,
		SignalingURL: "http://localhost:8080",
		RelayAddr:    "localhost:8443",
		AlwaysRelay:  true,
		Debug:        false,
	}
}

// TargetAddr returns the full target address
func (c *Config) TargetAddr() string {
	return net.JoinHostPort(c.TargetHost, strconv.Itoa(c.TargetPort))
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.TargetPort < 1 || c.TargetPort > 65535 {
		return fmt.Errorf("invalid target port: %d", c.TargetPort)
	}
	if c.SignalingURL == "" {
		return fmt.Errorf("signaling URL is required")
	}
	if c.RelayAddr == "" {
		return fmt.Errorf("relay address is required")
	}
	return nil
}

// LoadFromEnv loads configuration from environment variables
func (c *Config) LoadFromEnv() {
	if host := os.Getenv("SFO_TARGET_HOST"); host != "" {
		c.TargetHost = host
	}
	if port := os.Getenv("SFO_TARGET_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			c.TargetPort = p
		}
	}
	if url := os.Getenv("SFO_SIGNALING_URL"); url != "" {
		c.SignalingURL = url
	}
	if addr := os.Getenv("SFO_RELAY_ADDR"); addr != "" {
		c.RelayAddr = addr
	}
	if debug := os.Getenv("SFO_DEBUG"); debug == "true" || debug == "1" {
		c.Debug = true
	}
}
