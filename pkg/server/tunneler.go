package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/aojea/rwconn"
	"golang.org/x/net/http2"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

func startTunneler(ctx context.Context, upstreamUrl, downstreamUrl *url.URL) error {
	proxy := httputil.NewSingleHostReverseProxy(downstreamUrl)
	proxy.Transport = http.DefaultTransport

	l, err := NewListener(http.DefaultClient, upstreamUrl.String())
	if err != nil {
		return err
	}
	defer l.Close()

	// reverse proxy the request coming from the reverse connection to the p-cluster apiserver
	server := &http.Server{ReadHeaderTimeout: 30 * time.Second, Handler: downstreamWrapper(proxy)}
	defer server.Close()

	errCh := make(chan error)
	go func() {
		errCh <- server.Serve(l)
	}()

	select {
	case err = <-errCh:
	case <-ctx.Done():
		err = server.Close()
	}
	return err
}

func downstreamWrapper(handler http.Handler) http.HandlerFunc {

	return func(w http.ResponseWriter, req *http.Request) {

		handler.ServeHTTP(w, req)
	}
}

var _ net.Listener = (*Listener)(nil)

// Listener is a net.Listener, returning new connections which arrive
// from a corresponding Dialer.
type Listener struct {
	url    string
	client *http.Client

	sc     net.Conn // control plane connection
	connc  chan net.Conn
	donec  chan struct{}
	writec chan<- []byte

	mu      sync.Mutex // guards below, closing connc, and writing to rw
	readErr error
	closed  bool
}

// NewListener returns a new Listener, it dials to the Dialer
// creating "reverse connection" that are accepted by this Listener.
// - client: http client, required for TLS
// - url: a URL to the base of the reverse handler on the Dialer.
func NewListener(client *http.Client, url string) (*Listener, error) {
	err := configureHTTP2Transport(client)
	if err != nil {
		return nil, err
	}

	ln := &Listener{
		url:    url,
		client: client,
		connc:  make(chan net.Conn, 4), // arbitrary
		donec:  make(chan struct{}),
	}

	// create control plane connection
	// poor man backoff retry
	sleep := 1 * time.Second
	var c net.Conn
	for attempts := 5; attempts > 0; attempts-- {
		c, err = ln.dial()
		if err != nil {
			// Add some randomness to prevent creating a Thundering Herd
			jitter := time.Duration(rand.Int63n(int64(sleep)))
			sleep = 2*sleep + jitter/2
			time.Sleep(sleep)
		} else {
			ln.sc = c
			break
		}
	}
	if c == nil || err != nil {
		return nil, err
	}

	go ln.run()
	return ln, nil
}

// run establish reverse connections against the server.
func (ln *Listener) run() {
	defer ln.Close()

	// Write loop
	writec := make(chan []byte, 8)
	ln.writec = writec
	go func() {
		for {
			select {
			case <-ln.donec:
				return
			case msg := <-writec:
				if _, err := ln.sc.Write(msg); err != nil {
					ln.Close()
					return
				}
			}
		}
	}()

	// Read loop
	br := bufio.NewReader(ln.sc)
	for {
		line, err := br.ReadSlice('\n')
		if err != nil {
			return
		}
		var msg controlMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			return
		}
		switch msg.Command {
		case "keep-alive":
			// Occasional no-op message from server to keep
			// us alive through NAT timeouts.
		case "conn-ready":
			go ln.grabConn()
		default:
			// Ignore unknown messages
		}
	}
}

func (ln *Listener) sendMessage(m controlMsg) {
	j, _ := json.Marshal(m) //nolint:errchkjson
	j = append(j, '\n')
	ln.writec <- j
}

func (ln *Listener) dial() (net.Conn, error) {
	connect := ln.url + "/" + "foo"
	pr, pw := io.Pipe()
	req, err := http.NewRequest(http.MethodGet, connect, pr) //nolint:noctx
	if err != nil {
		return nil, err
	}

	res, err := ln.client.Do(req) //nolint:bodyclose // Seems we're returning the connection with res.Body, caller closes it?
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code %d", res.StatusCode)
	}

	conn := rwconn.NewConn(res.Body, pw)
	return conn, nil
}

func (ln *Listener) grabConn() {
	// create a new connection
	c, err := ln.dial()
	if err != nil {
		ln.sendMessage(controlMsg{Command: "pickup-failed", ConnPath: "", Err: err.Error()})
		return
	}

	// send the connection to the listener
	select {
	case <-ln.donec:
		return
	default:
		select {
		case ln.connc <- c:
		case <-ln.donec:
			return
		}
	}
}

// Accept blocks and returns a new connection, or an error.
func (ln *Listener) Accept() (net.Conn, error) {
	c, ok := <-ln.connc
	if !ok {
		ln.mu.Lock()
		err, closed := ln.readErr, ln.closed
		ln.mu.Unlock()
		if err != nil && !closed {
			return nil, fmt.Errorf("tunneler: Listener closed; %w", err)
		}
		return nil, ErrListenerClosed
	}
	return c, nil
}

// ErrListenerClosed is returned by Accept after Close has been called.
var ErrListenerClosed = fmt.Errorf("tunneler: Listener closed")

// Close closes the Listener, making future Accept calls return an
// error.
func (ln *Listener) Close() error {
	ln.mu.Lock()
	defer ln.mu.Unlock()
	if ln.closed {
		return nil
	}
	ln.closed = true
	close(ln.connc)
	close(ln.donec)
	ln.sc.Close()
	return nil
}

// Addr returns a dummy address. This exists only to conform to the
// net.Listener interface.
func (ln *Listener) Addr() net.Addr { return connAddr{} }

// configureHTTP2Transport enable ping to avoid issues with stale connections.
func configureHTTP2Transport(client *http.Client) error {
	t, ok := client.Transport.(*http.Transport)
	if !ok {
		// can't get the transport it will fail later if not http2 supported
		return nil
	}

	if t.TLSClientConfig == nil {
		return fmt.Errorf("only TLS supported")
	}

	for _, v := range t.TLSClientConfig.NextProtos {
		// http2 already configured
		if v == "h2" {
			return nil
		}
	}

	t2, err := http2.ConfigureTransports(t)
	if err != nil {
		return err
	}

	t2.ReadIdleTimeout = time.Duration(30) * time.Second
	t2.PingTimeout = time.Duration(15) * time.Second
	return nil
}

type connAddr struct{}

func (connAddr) Network() string { return "rwconn" }
func (connAddr) String() string  { return "rwconn" }

type controlMsg struct {
	Command  string `json:"command,omitempty"`  // "keep-alive", "conn-ready", "pickup-failed"
	ConnPath string `json:"connPath,omitempty"` // conn pick-up URL path for "conn-url", "pickup-failed"
	Err      string `json:"err,omitempty"`
}
