// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Fabrintek Engenharia Digital Ltda

// Package testnet gives tests loopback ports that nothing else takes from
// under them: STUN and TURN servers answer UDP and TCP on one port number.
package testnet

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	attempts = 50
	// minFixedPort and the kernel's ephemeral range bound the ports handed to
	// servers that take a port number: the kernel never gives these out to
	// port-0 binds or outgoing connections.
	minFixedPort         = 20000
	defaultEphemeralLow  = 32768
	ephemeralRangeConfig = "/proc/sys/net/ipv4/ip_local_port_range"
)

// ListenUDPAndTCP opens a UDP socket and a TCP listener on the same loopback
// port and keeps both open, for an in-process server to take over.
func ListenUDPAndTCP() (net.PacketConn, net.Listener, error) {
	return listenPair(func() (net.PacketConn, error) {
		return net.ListenPacket("udp4", "127.0.0.1:0")
	})
}

func listenPair(nextUDP func() (net.PacketConn, error)) (net.PacketConn, net.Listener, error) {
	var errs []error
	for range attempts {
		udp, err := nextUDP()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		port := udp.LocalAddr().(*net.UDPAddr).Port
		tcp, err := net.Listen("tcp4", loopback(port))
		if err == nil {
			return udp, tcp, nil
		}
		_ = udp.Close()
		errs = append(errs, err)
	}
	return nil, nil, fmt.Errorf("no loopback port free for both UDP and TCP: %w", errors.Join(errs...))
}

// FreeUDPAndTCPPort finds a loopback port free for both UDP and TCP, for an
// external server that must be given a port number. It is chosen below the
// ephemeral range, so between its release here and the server's bind only
// another explicit bind to that very number can take it.
func FreeUDPAndTCPPort() (int, error) {
	low := ephemeralLow()
	var errs []error
	for range attempts {
		port := minFixedPort + rand.IntN(low-minFixedPort) // #nosec G404 -- spreads test ports, not a secret
		udpErr := bindAndRelease("udp4", port)
		tcpErr := bindAndRelease("tcp4", port)
		if udpErr == nil && tcpErr == nil {
			return port, nil
		}
		errs = append(errs, udpErr, tcpErr)
	}
	return 0, fmt.Errorf("no fixed loopback port free for both UDP and TCP: %w", errors.Join(errs...))
}

func bindAndRelease(network string, port int) error {
	if strings.HasPrefix(network, "udp") {
		c, err := net.ListenPacket(network, loopback(port))
		if err != nil {
			return err
		}
		return c.Close()
	}
	l, err := net.Listen(network, loopback(port))
	if err != nil {
		return err
	}
	return l.Close()
}

func ephemeralLow() int {
	b, err := os.ReadFile(ephemeralRangeConfig)
	if err != nil {
		return defaultEphemeralLow
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return defaultEphemeralLow
	}
	low, err := strconv.Atoi(fields[0])
	if err != nil || low <= minFixedPort {
		return defaultEphemeralLow
	}
	return low
}

func loopback(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
