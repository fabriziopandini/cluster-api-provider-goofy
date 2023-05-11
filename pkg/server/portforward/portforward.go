package portforward

import (
	"context"
	"fmt"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"log"
	"strconv"
	"sync"
	"time"
)

// HttpStreamReceived is the httpstream.NewStreamHandler for port
// forward streams. It checks each stream's port and stream type headers,
// rejecting any streams that with missing or invalid values. Each valid
// stream is sent to the streams channel.
func HttpStreamReceived(streamsCh chan httpstream.Stream) func(httpstream.Stream, <-chan struct{}) error {
	return func(stream httpstream.Stream, replySent <-chan struct{}) error {
		// make sure it has a valid port header
		portString := stream.Headers().Get(corev1.PortHeader)
		if len(portString) == 0 {
			return fmt.Errorf("%q header is required", corev1.PortHeader)
		}
		port, err := strconv.ParseUint(portString, 10, 16)
		if err != nil {
			return fmt.Errorf("unable to parse %q as a port: %w", portString, err)
		}
		if port < 1 {
			return fmt.Errorf("port %q must be > 0", portString)
		}

		// make sure it has a valid stream type header
		streamType := stream.Headers().Get(corev1.StreamType)
		if len(streamType) == 0 {
			return fmt.Errorf("%q header is required", corev1.StreamType)
		}
		if streamType != corev1.StreamTypeError && streamType != corev1.StreamTypeData {
			return fmt.Errorf("invalid stream type %q", streamType)
		}

		streamsCh <- stream
		return nil
	}
}

func NewHttpStreamHandler(conn httpstream.Connection, streamsCh chan httpstream.Stream, podName, podNamespace string, forwarder PortForwarder) HttpStreamHandler {
	return &httpStreamHandler{
		conn:                  conn,
		streamChan:            streamsCh,
		streamPairs:           make(map[string]*httpStreamPair),
		streamCreationTimeout: 30 * time.Second,
		podName:               podName,
		podNamespace:          podNamespace,
		forwarder:             forwarder,
	}
}

type HttpStreamHandler interface {
	Run(ctx context.Context)
}

// httpStreamHandler is capable of processing multiple port forward
// requests over a single httpstream.Connection.
type httpStreamHandler struct {
	logger                *log.Logger
	conn                  httpstream.Connection
	streamChan            chan httpstream.Stream
	streamPairsLock       sync.RWMutex
	streamPairs           map[string]*httpStreamPair
	streamCreationTimeout time.Duration
	podName               string
	podNamespace          string
	forwarder             PortForwarder
}

// PortForwarder knows how to forward content from a data stream to/from a port
// in a pod.
type PortForwarder interface {
	// PortForwarder copies data between a data stream and a port in a pod.
	PortForward(ctx context.Context, podName, podNamespace string, port int32, stream io.ReadWriteCloser) error
}

// getStreamPair returns a httpStreamPair for requestID. This creates a
// new pair if one does not yet exist for the requestID. The returned bool is
// true if the pair was created.
func (h *httpStreamHandler) getStreamPair(requestID string) (*httpStreamPair, bool) {
	h.streamPairsLock.Lock()
	defer h.streamPairsLock.Unlock()

	if p, ok := h.streamPairs[requestID]; ok {
		log.Println("Connection request found existing stream pair", "connection", h.conn, "request", requestID)
		return p, false
	}

	log.Println("Connection request creating new stream pair", "connection", h.conn, "request", requestID)

	p := newPortForwardPair(requestID)
	h.streamPairs[requestID] = p

	return p, true
}

// monitorStreamPair waits for the pair to receive both its error and data
// streams, or for the timeout to expire (whichever happens first), and then
// removes the pair.
func (h *httpStreamHandler) monitorStreamPair(p *httpStreamPair, timeout <-chan time.Time) {
	select {
	case <-timeout:
		err := fmt.Errorf("(conn=%v, request=%s) timed out waiting for streams", h.conn, p.requestID)
		log.Println("timeout", err.Error())
		p.printError(err.Error())
	case <-p.complete:
		log.Println("Connection request successfully received error and data streams", "connection", h.conn, "request", p.requestID)
	}
	h.removeStreamPair(p.requestID)
}

// hasStreamPair returns a bool indicating if a stream pair for requestID
// exists.
func (h *httpStreamHandler) hasStreamPair(requestID string) bool {
	h.streamPairsLock.RLock()
	defer h.streamPairsLock.RUnlock()

	_, ok := h.streamPairs[requestID]
	return ok
}

// removeStreamPair removes the stream pair identified by requestID from streamPairs.
func (h *httpStreamHandler) removeStreamPair(requestID string) {
	h.streamPairsLock.Lock()
	defer h.streamPairsLock.Unlock()

	if h.conn != nil {
		pair := h.streamPairs[requestID]
		h.conn.RemoveStreams(pair.dataStream, pair.errorStream)
	}
	delete(h.streamPairs, requestID)
}

// requestID returns the request id for stream.
func (h *httpStreamHandler) requestID(stream httpstream.Stream) string {
	requestID := stream.Headers().Get(corev1.PortForwardRequestIDHeader)
	if len(requestID) == 0 {
		log.Println("Connection stream received without requestID header", "connection", h.conn)
		// If we get here, it's because the connection came from an older client
		// that isn't generating the request id header
		// (https://github.com/kubernetes/kubernetes/blob/843134885e7e0b360eb5441e85b1410a8b1a7a0c/pkg/client/unversioned/portforward/portforward.go#L258-L287)
		//
		// This is a best-effort attempt at supporting older clients.
		//
		// When there aren't concurrent new forwarded connections, each connection
		// will have a pair of streams (data, error), and the stream IDs will be
		// consecutive odd numbers, e.g. 1 and 3 for the first connection. Convert
		// the stream ID into a pseudo-request id by taking the stream type and
		// using id = stream.Identifier() when the stream type is error,
		// and id = stream.Identifier() - 2 when it's data.
		//
		// NOTE: this only works when there are not concurrent new streams from
		// multiple forwarded connections; it's a best-effort attempt at supporting
		// old clients that don't generate request ids.  If there are concurrent
		// new connections, it's possible that 1 connection gets streams whose IDs
		// are not consecutive (e.g. 5 and 9 instead of 5 and 7).
		streamType := stream.Headers().Get(corev1.StreamType)
		switch streamType {
		case corev1.StreamTypeError:
			requestID = strconv.Itoa(int(stream.Identifier()))
		case corev1.StreamTypeData:
			requestID = strconv.Itoa(int(stream.Identifier()) - 2)
		}

		log.Println("Connection automatically assigning request ID from stream type and stream ID", "connection", h.conn, "request", requestID, "streamType", streamType, "stream", stream.Identifier())
	}
	return requestID
}

// Run is the main loop for the HttpStreamHandler. It processes new
// streams, invoking portForward for each complete stream pair. The loop exits
// when the httpstream.Connection is closed.
func (h *httpStreamHandler) Run(ctx context.Context) {
	log.Println("Connection waiting for port forward streams", "connection", h.conn)
Loop:
	for {
		select {
		case <-h.conn.CloseChan():
			log.Println("Connection upgraded connection closed", "connection", h.conn)
			break Loop
		case stream := <-h.streamChan:
			requestID := h.requestID(stream)
			streamType := stream.Headers().Get(corev1.StreamType)
			log.Println("Connection request received new type of stream", "connection", h.conn, "request", requestID, "streamType", streamType)

			p, created := h.getStreamPair(requestID)
			if created {
				go h.monitorStreamPair(p, time.After(h.streamCreationTimeout))
			}
			if complete, err := p.add(stream); err != nil {
				err := fmt.Errorf("error processing stream for request %s: %w", requestID, err)
				log.Println("add stream", err.Error())
				p.printError(err.Error())
			} else if complete {
				go h.portForward(ctx, p)
			}
		}
	}
}

// portForward invokes the HttpStreamHandler's forwarder.PortForward
// function for the given stream pair.
func (h *httpStreamHandler) portForward(ctx context.Context, p *httpStreamPair) {
	defer func() {
		_ = p.errorStream.Close()
		_ = p.dataStream.Close()
	}()

	portString := p.dataStream.Headers().Get(corev1.PortHeader)
	port, _ := strconv.ParseInt(portString, 10, 32)

	log.Println("Connection request invoking forwarder.PortForward for port", "connection", h.conn, "request", p.requestID, "port", portString)
	err := h.forwarder.PortForward(ctx, h.podName, h.podNamespace, int32(port), p.dataStream)
	log.Println("Connection request done invoking forwarder.PortForward for port", "connection", h.conn, "request", p.requestID, "port", portString)

	if err != nil {
		err := fmt.Errorf("error forwarding port %d to pod %s/%s: %w", port, h.podNamespace, h.podName, err)
		log.Println("PortForward", err.Error())
		fmt.Fprint(p.errorStream, err.Error())
	}
}

// httpStreamPair represents the error and data streams for a port
// forwarding request.
type httpStreamPair struct {
	lock        sync.RWMutex
	requestID   string
	dataStream  httpstream.Stream
	errorStream httpstream.Stream
	complete    chan struct{}
}

// newPortForwardPair creates a new httpStreamPair.
func newPortForwardPair(requestID string) *httpStreamPair {
	return &httpStreamPair{
		requestID: requestID,
		complete:  make(chan struct{}),
	}
}

// add adds the stream to the httpStreamPair. If the pair already
// contains a stream for the new stream's type, an error is returned. add
// returns true if both the data and error streams for this pair have been
// received.
func (p *httpStreamPair) add(stream httpstream.Stream) (bool, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	switch stream.Headers().Get(corev1.StreamType) {
	case corev1.StreamTypeError:
		if p.errorStream != nil {
			return false, fmt.Errorf("error stream already assigned")
		}
		p.errorStream = stream
	case corev1.StreamTypeData:
		if p.dataStream != nil {
			return false, fmt.Errorf("data stream already assigned")
		}
		p.dataStream = stream
	}

	complete := p.errorStream != nil && p.dataStream != nil
	if complete {
		close(p.complete)
	}
	return complete, nil
}

// printError writes s to p.errorStream if p.errorStream has been set.
func (p *httpStreamPair) printError(s string) {
	p.lock.RLock()
	defer p.lock.RUnlock()
	if p.errorStream != nil {
		fmt.Fprint(p.errorStream, s)
	}
}
