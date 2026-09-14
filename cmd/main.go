package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"gophre/env"
	"gophre/pkg/cli"
)

// forceIPv4 rewires the process-wide default HTTP transport so every outbound
// connection (RSS fetches, Discord webhook, GitHub OAuth) dials over IPv4
// only. When LOCAL_IPV4 is set, outbound connections are also pinned to that
// local address.
func forceIPv4() {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	if env.LOCAL_IPV4 != "" {
		ip := net.ParseIP(env.LOCAL_IPV4)
		if ip == nil || ip.To4() == nil {
			log.Printf("Ignoring invalid LOCAL_IPV4 value: %q.\n", env.LOCAL_IPV4)
		} else {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", addr)
	}
	http.DefaultTransport = transport
}

func main() {
	forceIPv4()
	cli.ParseCommand()
}
