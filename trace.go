package shttp

import (
	"context"
	"crypto/tls"
	"net"
	"net/http/httptrace"
	"time"
)

type TraceInfo struct {
	// DNSLookup is a duration that transport took to perform
	DNSLookup time.Duration
	// ConnTime is a duration that took to obtain a successful connection.
	ConnTime time.Duration
	// TCPConnTime is a duration that took to obtain the TCP connection.
	TCPConnTime time.Duration
	// TLSHandshake is a duration that TLS handshake took place.
	TLSHandshake time.Duration
	// ServerTime is a duration that server took to respond first byte.
	ServerTime time.Duration
	// ResponseTime is a duration since first response byte from server to
	// request completion.
	ResponseTime time.Duration
	// TotalTime is a duration that total request took end-to-end.
	TotalTime time.Duration
	// IsConnReused is whether this connection has been previously
	// used for another HTTP request.
	IsConnReused bool
	// IsConnWasIdle is whether this connection was obtained from an
	// idle pool.
	IsConnWasIdle bool
	// ConnIdleTime is a duration how long the connection was previously
	// idle, if IsConnWasIdle is true.
	ConnIdleTime time.Duration
	// RequestAttempt is to represent the request attempt made during a Resty
	// request execution flow, including retry count.
	//RequestAttempt int
	// RemoteAddr returns the remote network address.
	RemoteAddr net.Addr
}

// tracer struct maps the `httptrace.ClientTrace` hooks into Fields
// with same naming for easy understanding. Plus additional insights
// Request.
type clientTrace struct {
	getConn              time.Time
	dnsStart             time.Time
	dnsDone              time.Time
	connectDone          time.Time
	tlsHandshakeStart    time.Time
	tlsHandshakeDone     time.Time
	gotConn              time.Time
	gotFirstResponseByte time.Time
	endTime              time.Time
	gotConnInfo          httptrace.GotConnInfo
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Trace unexported methods
//_______________________________________________________________________

func (t *clientTrace) createContext(ctx context.Context) context.Context {
	return httptrace.WithClientTrace(
		ctx,
		&httptrace.ClientTrace{
			DNSStart: func(_ httptrace.DNSStartInfo) {
				t.dnsStart = time.Now()
			},
			DNSDone: func(_ httptrace.DNSDoneInfo) {
				t.dnsDone = time.Now()
			},
			ConnectStart: func(_, _ string) {
				if t.dnsDone.IsZero() {
					t.dnsDone = time.Now()
				}
				if t.dnsStart.IsZero() {
					t.dnsStart = t.dnsDone
				}
			},
			ConnectDone: func(net, addr string, err error) {
				t.connectDone = time.Now()
			},
			GetConn: func(_ string) {
				t.getConn = time.Now()
			},
			GotConn: func(ci httptrace.GotConnInfo) {
				t.gotConn = time.Now()
				t.gotConnInfo = ci
			},
			GotFirstResponseByte: func() {
				t.gotFirstResponseByte = time.Now()
			},
			TLSHandshakeStart: func() {
				t.tlsHandshakeStart = time.Now()
			},
			TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
				t.tlsHandshakeDone = time.Now()
			},
		},
	)
}