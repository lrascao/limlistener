package limlistener

import (
	"context"
	"net"
	"sync"
	"time"

	rlimit "golang.org/x/time/rate"
)

const (
	//  this is the default block size sent
	defaultMTU = 1024
)

// LimitedConn satisfies the net.Conn interface and adds
// throttling functionality to downstream traffic (ie. Write method)
type LimitedConn struct {
	// connection id
	id int
	// the underlying connection
	conn          net.Conn
	globalLimiter *rlimit.Limiter
	connLimiter   *rlimit.Limiter
	mtu           int
}

// LimitedListener satisfies the net.Listener interface
// and adds both global and per-connection bandwidth throttling
// functionality
type LimitedListener struct {
	n_conns       int
	listener      net.Listener
	globalLimiter *rlimit.Limiter
	connLimit     int
	mtu           int
	conns         []*LimitedConn
}

// NewWithListener takes an existing net.Listener and creates a new
// LimitedListener that is in charge of throttling bandwidth both
// per connection and globally
func NewWithListener(l net.Listener) LimitedListener {
	return LimitedListener{
		n_conns:  0,
		listener: l,
		mtu:      defaultMTU,
	}
}

// SetLimits defines both new global and per-connection limits
func (ll *LimitedListener) SetLimits(globalLimit, connLimit int) {
	// had we already created a rate limiter?
	if ll.globalLimiter == nil {
		ll.globalLimiter = rlimit.NewLimiter(rlimit.Limit(globalLimit), ll.mtu)
	}
	// set the global limiter
	ll.globalLimiter.SetLimit(rlimit.Limit(globalLimit))
	// keep memory of the new connection limit
	ll.connLimit = connLimit
	// update each running connection's rate limiter
	for _, conn := range ll.conns {
		conn.SetLimit(connLimit)
	}
}

// Accept creates a new limited connection that will throttle the
// bandwidth both at a connection level and at aggregate that will depend
// on how many connections are open
func (ll *LimitedListener) Accept() (net.Conn, error) {
	conn, err := ll.listener.Accept()
	// each connection gets the global limiter and a per-connection one
	conn_id := ll.n_conns
	ll.n_conns++
	lconn := LimitedConn{
		id:            conn_id,
		conn:          conn,
		globalLimiter: ll.globalLimiter,
		connLimiter:   rlimit.NewLimiter(rlimit.Limit(ll.connLimit), ll.mtu),
		mtu:           ll.mtu,
	}
	// keep a pointer to the limited connection
	ll.conns = append(ll.conns, &lconn)
	return lconn, err
}

// CloseConnection cleans up a specific connection, should be used
// instead of net.Conn.Close as it cleans up additional resources
func (ll *LimitedListener) CloseConnection(conn net.Conn) {
	// type conversion
	lconn := conn.(LimitedConn)
	conn.Close()
	for i, c := range ll.conns {
		if c.id == lconn.id {
			// remove the entry from the slice
			ll.conns[i] = ll.conns[len(ll.conns)-1]
			ll.conns = ll.conns[:len(ll.conns)-1]
			break
		}
	}
	ll.n_conns--
}

func (lc LimitedConn) waitN(ctx context.Context, n int) error {
	// wait concurrently for both limiters to allow progress
	var wg sync.WaitGroup
	done := make(chan error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		err := lc.connLimiter.WaitN(ctx, n)
		done <- err
	}()
	go func() {
		defer wg.Done()
		err := lc.globalLimiter.WaitN(ctx, n)
		done <- err
	}()

	// wait on both goroutines to be done
	// then close the channel so ranging stops
	go func() {
		wg.Wait()
		close(done)
	}()

	// was there an error?
	for err := range done {
		if err != nil {
			return err
		}
	}
	return nil
}

// Write asks permission for both global and per-connection rate limiters
// before pushing down MTU worth of bytes down the pipe.
func (lc LimitedConn) Write(b []byte) (n int, err error) {
	n = 0
	ctx := context.Background()
	// while there's still something to write
	for len(b) > 0 {
		var s []byte
		// pop the first MTU bytes
		s = b
		if len(b) > lc.mtu {
			s = b[:lc.mtu]
		}
		// move slice past MTU
		b = b[len(s):]

		// get permission to write it
		err = lc.waitN(ctx, len(s))
		if err != nil {
			return 0, err
		}

		// push it down the pipe
		w, err := lc.conn.Write(s)
		if err != nil {
			return 0, err
		}
		n += w
	}

	return n, err
}

func (lc *LimitedConn) SetLimit(limit int) {
	// had we already created a rate limiter?
	if lc.connLimiter == nil {
		lc.connLimiter = rlimit.NewLimiter(rlimit.Limit(limit), lc.mtu)
	}
	// set the new limit
	lc.connLimiter.SetLimit(rlimit.Limit(limit))
}

// Close calls to net.Listener.Close()
func (ll LimitedListener) Close() error {
	return ll.listener.Close()
}

// Addr calls to net.Listener.Addr()
func (ll LimitedListener) Addr() net.Addr {
	return ll.listener.Addr()
}

// Read calls to net.Conn.Read()
func (lc LimitedConn) Read(b []byte) (n int, err error) {
	return lc.conn.Read(b)
}

// Close calls to net.Conn.Close()
func (lc LimitedConn) Close() error {
	return lc.conn.Close()
}

// LocalAddr calls to net.Conn.LocalAddr()
func (lc LimitedConn) LocalAddr() net.Addr {
	return lc.conn.LocalAddr()
}

// RemoteAddr calls to net.Conn.RemoteAddr()
func (lc LimitedConn) RemoteAddr() net.Addr {
	return lc.conn.RemoteAddr()
}

// SetDeadline calls to net.Conn.SetDeadline()
func (lc LimitedConn) SetDeadline(t time.Time) error {
	return lc.conn.SetDeadline(t)
}

// SetReadDeadline calls to net.Conn.SetReadDeadline()
func (lc LimitedConn) SetReadDeadline(t time.Time) error {
	return lc.conn.SetReadDeadline(t)
}

// SetWriteDeadline calls to net.Conn.SetWriteDeadline()
func (lc LimitedConn) SetWriteDeadline(t time.Time) error {
	return lc.conn.SetWriteDeadline(t)
}
