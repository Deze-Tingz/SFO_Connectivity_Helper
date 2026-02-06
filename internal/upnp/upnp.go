package upnp

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

// PortMapping represents a UPnP port mapping
type PortMapping struct {
	Protocol    string // "TCP" or "UDP"
	ExternalPort int
	InternalPort int
	InternalIP   string
	Description  string
	Duration     uint32 // 0 = permanent until removed
}

// UPnPClient handles UPnP port forwarding
type UPnPClient struct {
	clients1 []*internetgateway1.WANIPConnection1
	clients2 []*internetgateway2.WANIPConnection1
	localIP  string
}

// NewUPnPClient discovers UPnP devices and creates a client
func NewUPnPClient() (*UPnPClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &UPnPClient{}

	// Get local IP
	client.localIP = getLocalIP()

	// Try IGD v2 first
	clients2, _, _ := internetgateway2.NewWANIPConnection1ClientsCtx(ctx)
	client.clients2 = clients2

	// Also try IGD v1
	clients1, _, _ := internetgateway1.NewWANIPConnection1ClientsCtx(ctx)
	client.clients1 = clients1

	if len(clients1) == 0 && len(clients2) == 0 {
		return nil, fmt.Errorf("no UPnP gateway found")
	}

	return client, nil
}

// AddPortMapping adds a port mapping
func (c *UPnPClient) AddPortMapping(mapping PortMapping) error {
	if mapping.InternalIP == "" {
		mapping.InternalIP = c.localIP
	}

	var lastErr error

	// Try IGD v2
	for _, client := range c.clients2 {
		err := client.AddPortMapping(
			"",
			uint16(mapping.ExternalPort),
			mapping.Protocol,
			uint16(mapping.InternalPort),
			mapping.InternalIP,
			true,
			mapping.Description,
			mapping.Duration,
		)
		if err == nil {
			return nil
		}
		lastErr = err
	}

	// Try IGD v1
	for _, client := range c.clients1 {
		err := client.AddPortMapping(
			"",
			uint16(mapping.ExternalPort),
			mapping.Protocol,
			uint16(mapping.InternalPort),
			mapping.InternalIP,
			true,
			mapping.Description,
			mapping.Duration,
		)
		if err == nil {
			return nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return fmt.Errorf("failed to add port mapping: %w", lastErr)
	}
	return fmt.Errorf("no UPnP clients available")
}

// RemovePortMapping removes a port mapping
func (c *UPnPClient) RemovePortMapping(externalPort int, protocol string) error {
	var lastErr error

	// Try IGD v2
	for _, client := range c.clients2 {
		err := client.DeletePortMapping("", uint16(externalPort), protocol)
		if err == nil {
			return nil
		}
		lastErr = err
	}

	// Try IGD v1
	for _, client := range c.clients1 {
		err := client.DeletePortMapping("", uint16(externalPort), protocol)
		if err == nil {
			return nil
		}
		lastErr = err
	}

	return lastErr
}

// GetExternalIP returns the external IP address
func (c *UPnPClient) GetExternalIP() (string, error) {
	// Try IGD v2
	for _, client := range c.clients2 {
		ip, err := client.GetExternalIPAddress()
		if err == nil && ip != "" {
			return ip, nil
		}
	}

	// Try IGD v1
	for _, client := range c.clients1 {
		ip, err := client.GetExternalIPAddress()
		if err == nil && ip != "" {
			return ip, nil
		}
	}

	return "", fmt.Errorf("failed to get external IP")
}

// OpenSFOPorts opens all ports needed for SFO (1626-1628 TCP/UDP)
func (c *UPnPClient) OpenSFOPorts(basePort int) error {
	ports := []struct {
		offset   int
		protocol string
		desc     string
	}{
		{0, "TCP", "SFO Game TCP"},
		{1, "TCP", "SFO Relay TCP"},
		{2, "TCP", "SFO Signal TCP"},
		{0, "UDP", "SFO Game UDP"},
		{1, "UDP", "SFO Relay UDP"},
		{2, "UDP", "SFO Signal UDP"},
	}

	for _, p := range ports {
		mapping := PortMapping{
			Protocol:     p.protocol,
			ExternalPort: basePort + p.offset,
			InternalPort: basePort + p.offset,
			Description:  p.desc,
			Duration:     0, // Permanent until removed
		}
		if err := c.AddPortMapping(mapping); err != nil {
			// Log but continue - some mappings might already exist
			fmt.Printf("  Warning: Could not map port %d/%s: %v\n", mapping.ExternalPort, p.protocol, err)
		}
	}

	return nil
}

// CloseSFOPorts closes all SFO ports
func (c *UPnPClient) CloseSFOPorts(basePort int) {
	protocols := []string{"TCP", "UDP"}
	for _, proto := range protocols {
		for i := 0; i < 3; i++ {
			c.RemovePortMapping(basePort+i, proto)
		}
	}
}

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// DiscoverGateway returns info about discovered gateway
func DiscoverGateway() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	devices, err := goupnp.DiscoverDevicesCtx(ctx, internetgateway2.URN_WANIPConnection_1)
	if err != nil || len(devices) == 0 {
		devices, err = goupnp.DiscoverDevicesCtx(ctx, internetgateway1.URN_WANIPConnection_1)
	}

	if err != nil {
		return "", err
	}

	if len(devices) > 0 {
		return devices[0].Root.Device.FriendlyName, nil
	}

	return "", fmt.Errorf("no gateway found")
}
