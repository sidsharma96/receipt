// Standalone diagnostic. No HTTP, credentials, response addresses, or packet bodies are logged.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"net"
	"os"
	"runtime"
	"runtime/trace"
	"sync"
	"time"
)

type event struct {
	Kind      string `json:"kind"`
	Start     int64  `json:"start_ns"`
	End       int64  `json:"end_ns"`
	Conn      int    `json:"conn"`
	Network   string `json:"network,omitempty"`
	Server    string `json:"server,omitempty"`
	Bytes     int    `json:"bytes,omitempty"`
	ID        uint16 `json:"id,omitempty"`
	Qtype     uint16 `json:"qtype,omitempty"`
	Rcode     uint16 `json:"rcode,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Failed    bool   `json:"failed"`
}

var mu sync.Mutex
var events []event
var next int
var base time.Time

func add(e event) {
	mu.Lock()
	defer mu.Unlock()
	if len(events) < 128 {
		events = append(events, e)
	}
}

type conn struct {
	net.Conn
	id      int
	network string
}

func (c *conn) Read(p []byte) (int, error) {
	s := time.Since(base).Nanoseconds()
	n, e := c.Conn.Read(p)
	end := time.Since(base).Nanoseconds()
	v := event{Kind: "read", Start: s, End: end, Conn: c.id, Bytes: n, Failed: e != nil}
	if c.network == "udp" && n >= 12 {
		v.ID = binary.BigEndian.Uint16(p)
		flags := binary.BigEndian.Uint16(p[2:])
		v.Rcode = flags & 15
		v.Truncated = flags&512 != 0
	}
	add(v)
	return n, e
}
func (c *conn) Write(p []byte) (int, error) {
	s := time.Since(base).Nanoseconds()
	n, e := c.Conn.Write(p)
	end := time.Since(base).Nanoseconds()
	v := event{Kind: "write", Start: s, End: end, Conn: c.id, Bytes: n, Failed: e != nil}
	if c.network == "udp" && len(p) >= 12 {
		v.ID = binary.BigEndian.Uint16(p)
		i := 12
		for i < len(p) && p[i] != 0 {
			l := int(p[i])
			if l > 63 {
				break
			}
			i += l + 1
		}
		if i+5 <= len(p) && p[i] == 0 {
			v.Qtype = binary.BigEndian.Uint16(p[i+1:])
		}
	}
	add(v)
	return n, e
}

// Preserve PacketConn recognition in net.dnsclient_unix exchange; wrapper must
// retain this interface or Go mistakes the UDP connection for a TCP stream.
type packetConn struct {
	*conn
	packet net.PacketConn
}

func (c *packetConn) ReadFrom(p []byte) (int, net.Addr, error)  { return c.packet.ReadFrom(p) }
func (c *packetConn) WriteTo(p []byte, a net.Addr) (int, error) { return c.packet.WriteTo(p, a) }
func main() {
	mode := flag.String("mode", "default", "default or instrumented-go")
	family := flag.String("family", "ip", "ip, ip4, ip6")
	rooted := flag.Bool("rooted", false, "trailing dot")
	tracePath := flag.String("trace", "", "optional runtime trace path")
	flag.Parse()
	if *mode != "default" && *mode != "instrumented-go" {
		os.Exit(2)
	}
	if *family != "ip" && *family != "ip4" && *family != "ip6" {
		os.Exit(2)
	}
	if *tracePath != "" {
		f, e := os.Create(*tracePath)
		if e != nil {
			os.Exit(2)
		}
		defer f.Close()
		if trace.Start(f) != nil {
			os.Exit(2)
		}
		defer trace.Stop()
	}
	base = time.Now()
	r := net.DefaultResolver
	if *mode == "instrumented-go" {
		r = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			mu.Lock()
			next++
			id := next
			mu.Unlock()
			s := time.Since(base).Nanoseconds()
			d := net.Dialer{}
			c, e := d.DialContext(ctx, network, address)
			add(event{Kind: "dial", Start: s, End: time.Since(base).Nanoseconds(), Conn: id, Network: network, Server: address, Failed: e != nil})
			if e != nil {
				return nil, e
			}
			wrapped := &conn{c, id, network}
			if p, ok := c.(net.PacketConn); ok {
				return &packetConn{wrapped, p}, nil
			}
			return wrapped, nil
		}}
	}
	host := "receipt.sidsharma.dev"
	if *rooted {
		host += "."
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	s := time.Since(base).Nanoseconds()
	ips, e := r.LookupIP(ctx, *family, host)
	end := time.Since(base).Nanoseconds()
	mu.Lock()
	copyEvents := append([]event(nil), events...)
	mu.Unlock()
	result := struct {
		Version string  `json:"go"`
		OS      string  `json:"os"`
		Arch    string  `json:"arch"`
		Mode    string  `json:"mode"`
		Family  string  `json:"family"`
		Rooted  bool    `json:"rooted"`
		UnixNS  int64   `json:"base_unix_ns"`
		Start   int64   `json:"start_ns"`
		End     int64   `json:"end_ns"`
		Answers int     `json:"answers"`
		Failed  bool    `json:"failed"`
		Events  []event `json:"events"`
	}{runtime.Version(), runtime.GOOS, runtime.GOARCH, *mode, *family, *rooted, base.UnixNano(), s, end, len(ips), e != nil, copyEvents}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		os.Exit(2)
	}
}
