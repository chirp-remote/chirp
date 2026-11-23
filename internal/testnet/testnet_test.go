// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Fabrintek Engenharia Digital Ltda

package testnet

import (
	"net"
	"strconv"
	"testing"
)

func TestListenUDPAndTCPSkipsAPortWhoseTCPSideIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()
	takenPort := taken.Addr().(*net.TCPAddr).Port

	tries := 0
	nextUDP := func() (net.PacketConn, error) {
		tries++
		if tries == 1 {
			return net.ListenPacket("udp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(takenPort)))
		}
		return net.ListenPacket("udp4", "127.0.0.1:0")
	}
	udp, tcp, err := listenPair(nextUDP)
	if err != nil {
		t.Fatalf("listenPair: %v", err)
	}
	defer func() { _ = udp.Close(); _ = tcp.Close() }()

	udpPort := udp.LocalAddr().(*net.UDPAddr).Port
	tcpPort := tcp.Addr().(*net.TCPAddr).Port
	if udpPort == takenPort {
		t.Errorf("listenPair kept port %d, whose TCP side another listener holds", takenPort)
	}
	if udpPort != tcpPort {
		t.Errorf("UDP port %d and TCP port %d differ", udpPort, tcpPort)
	}
}

func TestListenUDPAndTCPGivesUpAfterRepeatedFailures(t *testing.T) {
	taken, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()
	takenPort := taken.Addr().(*net.TCPAddr).Port

	nextUDP := func() (net.PacketConn, error) {
		return net.ListenPacket("udp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(takenPort)))
	}
	if udp, tcp, err := listenPair(nextUDP); err == nil {
		_ = udp.Close()
		_ = tcp.Close()
		t.Fatal("listenPair succeeded on a port whose TCP side is taken")
	}
}

func TestListenUDPAndTCPShareAPort(t *testing.T) {
	udp, tcp, err := ListenUDPAndTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = udp.Close(); _ = tcp.Close() }()
	if u, p := udp.LocalAddr().(*net.UDPAddr).Port, tcp.Addr().(*net.TCPAddr).Port; u != p {
		t.Errorf("UDP port %d and TCP port %d differ", u, p)
	}
}

func TestFreeUDPAndTCPPortIsBelowTheEphemeralRange(t *testing.T) {
	port, err := FreeUDPAndTCPPort()
	if err != nil {
		t.Fatal(err)
	}
	if low := ephemeralLow(); port < minFixedPort || port >= low {
		t.Errorf("port %d outside [%d, %d)", port, minFixedPort, low)
	}
	for _, network := range []string{"udp4", "tcp4"} {
		if err := bindAndRelease(network, port); err != nil {
			t.Errorf("%s port %d not free: %v", network, port, err)
		}
	}
}
