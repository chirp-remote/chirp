// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Fabrintek Engenharia Digital Ltda

// Command chirp-relay serves a Chirp relay: registration, codes and links,
// consent, and the signalling between the two ends of a session
// (signalling.Server).
//
// By default it is private: the relay its users run or trust, which the
// first runnable Chirp ships with (docs/product-model.md, decision 5). With
// -mode public it is a node of the public network
// (docs/status-page-proposal.md): it only looks up how an owner can be
// reached over Nostr, relays no session, and proves its identity at
// /.well-known/chirp-node with a key kept in -node-key. It speaks plain
// HTTP: put a TLS-terminating proxy in front of it for anything beyond one
// machine, and see docs/relay.md for running it and docs/stun-turn.md for the
// STUN and TURN servers a session also needs.
//
// The proxy reaches the relay best over a Unix socket (-addr unix:PATH),
// which only the processes its permissions admit can connect to: every
// request on it comes from the proxy, so its X-Forwarded-For is believed.
// Over TCP, name the proxy in -trusted-proxies instead, and keep everything
// else from reaching the port.
//
// Usage:
//
//	chirp-relay -addr localhost:8080
//	chirp-relay -addr unix:/run/chirp-relay/relay.sock
//	chirp-relay -addr 10.0.0.5:8080 -trusted-proxies 10.0.0.1
//	chirp-relay -mode public -node-key /var/lib/chirp-relay/node.key
//	chirp-relay -healthcheck
//
// A browser at the relay's address gets a page saying what it is
// (pages.go), robots.txt, and with -security-contact a security.txt.
//
// Each flag can also be set in the environment: CHIRP_RELAY_ADDR,
// CHIRP_RELAY_SOCKET_MODE, CHIRP_RELAY_TRUSTED_PROXIES,
// CHIRP_RELAY_SECURITY_CONTACT, CHIRP_RELAY_MODE, CHIRP_RELAY_NODE_KEY,
// CHIRP_RELAY_TURN_SECRET_FILE and CHIRP_RELAY_TURN_URLS. A flag given on the
// command line wins.
//
// With -turn-secret-file and -turn-urls, an owner gets short-lived TURN
// credentials for itself and the controller it lets in, checked by a coturn
// running use-auth-secret with the same secret (docs/stun-turn.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/barahn/chirp/node"
	"github.com/barahn/chirp/session"
	"github.com/barahn/chirp/signalling"
)

type relayFlags struct {
	addr        string
	socketMode  string
	proxies     string
	healthcheck bool
	contact     string
	mode        string
	nodeKey     string
	turnSecret  string
	turnURLs    string
}

type relaySettings struct {
	contact string
	mode    mode
	turn    *signalling.TURN
	trusted []netip.Prefix
}

func main() {
	f := parseFlags()
	if f.healthcheck {
		exitWithHealth(parseEndpoint(f.addr))
	}
	s, err := loadSettings(f)
	if err != nil {
		log.Fatalf("chirp-relay: %v", err)
	}
	ln, err := openListener(f)
	if err != nil {
		log.Fatalf("chirp-relay: %v", err)
	}

	sm := session.NewSessionManager(0)
	defer sm.Close()
	srv := &http.Server{
		Handler:           routes(newRelay(sm, s, ln).Handler(), s.contact, s.mode.identity),
		ReadHeaderTimeout: 10 * time.Second,
	}

	logStart(s.mode, s.turn)
	logListening(ln, s.trusted)
	if err := serveUntilSignalled(srv, ln); err != nil {
		log.Printf("chirp-relay: %v", err)
		sm.Close()
		//nolint:gocritic // sm.Close is called explicitly above before exiting
		os.Exit(1)
	}
}

func parseFlags() relayFlags {
	var f relayFlags
	flag.StringVar(&f.addr, "addr", envOr("CHIRP_RELAY_ADDR", "localhost:8080"), "address to listen on: host:port, or unix:PATH for a Unix socket")
	flag.StringVar(&f.socketMode, "socket-mode", envOr("CHIRP_RELAY_SOCKET_MODE", "0660"), "permissions of the Unix socket, in octal")
	flag.StringVar(&f.proxies, "trusted-proxies", os.Getenv("CHIRP_RELAY_TRUSTED_PROXIES"),
		"comma-separated addresses or CIDRs of the reverse proxies in front of the relay, whose X-Forwarded-For is believed")
	flag.BoolVar(&f.healthcheck, "healthcheck", false, "ask the relay listening on -addr whether it is up, and exit 0 if it is")
	flag.StringVar(&f.contact, "security-contact", os.Getenv("CHIRP_RELAY_SECURITY_CONTACT"),
		"where to report a security problem with this relay: an email address or an https: URL, served as /.well-known/security.txt")
	flag.StringVar(&f.mode, "mode", envOr("CHIRP_RELAY_MODE", "private"), "private, or public for a node of the public network")
	flag.StringVar(&f.nodeKey, "node-key", os.Getenv("CHIRP_RELAY_NODE_KEY"), "in public mode, the file holding the node's key; created on first start")
	flag.StringVar(&f.turnSecret, "turn-secret-file", os.Getenv("CHIRP_RELAY_TURN_SECRET_FILE"),
		"the file, mode 600, holding the secret coturn checks credentials with (static-auth-secret); with -turn-urls, owners get TURN credentials")
	flag.StringVar(&f.turnURLs, "turn-urls", os.Getenv("CHIRP_RELAY_TURN_URLS"), "comma-separated turn: URLs of that TURN server, handed out with each credential")
	flag.Parse()
	return f
}

func loadSettings(f relayFlags) (relaySettings, error) {
	var s relaySettings
	var err error
	if s.contact, err = securityContact(f.contact); err != nil {
		return s, fmt.Errorf("-security-contact: %w", err)
	}
	if s.mode, err = relayMode(f.mode, f.nodeKey); err != nil {
		return s, err
	}
	if s.turn, err = turnConfig(f.turnSecret, f.turnURLs); err != nil {
		return s, err
	}
	if s.trusted, err = signalling.ParsePrefixes(f.proxies); err != nil {
		return s, fmt.Errorf("-trusted-proxies: %w", err)
	}
	return s, nil
}

func openListener(f relayFlags) (net.Listener, error) {
	perm, err := socketFileMode(f.socketMode)
	if err != nil {
		return nil, err
	}
	return listen(parseEndpoint(f.addr), perm)
}

func socketFileMode(s string) (os.FileMode, error) {
	perm, err := strconv.ParseUint(s, 8, 32)
	if err != nil || perm > 0o777 {
		return 0, fmt.Errorf("-socket-mode: %q is not an octal permission", s)
	}
	return os.FileMode(perm), nil
}

func newRelay(sm *session.SessionManager, s relaySettings, ln net.Listener) *signalling.Server {
	relay := signalling.NewServer(sm)
	relay.LookupOnly = s.mode.public
	relay.TURN = s.turn
	if believesForwardedFor(ln, s.trusted) {
		relay.ClientIP = signalling.ForwardedFor(s.trusted)
	}
	return relay
}

func believesForwardedFor(ln net.Listener, trusted []netip.Prefix) bool {
	return ln.Addr().Network() == "unix" || len(trusted) > 0
}

func serveUntilSignalled(srv *http.Server, ln net.Listener) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type mode struct {
	public   bool
	identity http.Handler
	nodeKey  string
}

func relayMode(name, keyPath string) (mode, error) {
	switch name {
	case "private":
		return mode{}, nil
	case "public":
		if keyPath == "" {
			return mode{}, errors.New("-mode public needs -node-key, the file that holds the node's key")
		}
		key, err := node.LoadOrCreateKey(keyPath)
		if err != nil {
			return mode{}, fmt.Errorf("-node-key: %w", err)
		}
		return mode{public: true, identity: node.Handler(key), nodeKey: keyPath}, nil
	}
	return mode{}, fmt.Errorf("-mode: %q is neither private nor public", name)
}

// routes adds the relay's own health check, the pages a browser finds and,
// for a public node, its identity, in front of the signalling routes. Only
// the identity reads anything: the node's key.
func routes(relay http.Handler, contact string, identity http.Handler) http.Handler {
	mux := http.NewServeMux()
	pages(mux, contact, time.Now)
	if identity != nil {
		mux.Handle("GET "+node.Path, identity)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.Handle("/", relay)
	return mux
}

type endpoint struct {
	network string
	address string
}

func parseEndpoint(addr string) endpoint {
	if path, unix := strings.CutPrefix(addr, "unix:"); unix {
		return endpoint{network: "unix", address: path}
	}
	return endpoint{network: "tcp", address: addr}
}

func (e endpoint) isSocket() bool {
	return e.network == "unix"
}

// listen opens e. A Unix socket left behind by a relay that did not exit
// cleanly is replaced; any other file at that path is an error, not something
// to delete. The socket gets mode, whatever the umask.
func listen(e endpoint, mode os.FileMode) (net.Listener, error) {
	var lc net.ListenConfig
	if !e.isSocket() {
		return lc.Listen(context.Background(), e.network, e.address)
	}
	path := e.address
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode().Type() != os.ModeSocket {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	ln, err := lc.Listen(context.Background(), "unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, mode); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

// probe asks the relay listening on e for /healthz. It is what a container
// health check runs, since the relay's image has no shell or curl.
func probe(e endpoint) error {
	client, target, err := healthzClient(e)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+target+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: %s", resp.Status)
	}
	return nil
}

func healthzClient(e endpoint) (*http.Client, string, error) {
	transport := &http.Transport{}
	client := &http.Client{Timeout: 3 * time.Second, Transport: transport}
	if e.isSocket() {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, e.network, e.address)
		}
		return client, "relay", nil
	}
	target, err := loopbackTarget(e)
	return client, target, err
}

func loopbackTarget(e endpoint) (string, error) {
	host, port, err := net.SplitHostPort(e.address)
	if err != nil {
		return "", err
	}
	if listensOnEveryAddress(host) {
		host = "localhost"
	}
	return net.JoinHostPort(host, port), nil
}

func listensOnEveryAddress(host string) bool {
	switch host {
	case "", "0.0.0.0", "::":
		return true
	}
	return false
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
