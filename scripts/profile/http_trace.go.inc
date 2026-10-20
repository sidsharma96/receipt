// Diagnostic overlay fragment (.go.inc), appended to internal/cache/http.go by http_trace.py.
// Never compiled into the ordinary Receipt binary.
type diagnosticHTTP struct {
 mu sync.Mutex
 start time.Time
 events []diagnosticHTTPEvent
 kind string
 status int
 protocol string
 requestID string
 reused bool
 bytes int64
 body io.ReadCloser
}
type diagnosticHTTPEvent struct { Name string `json:"name"`; NS int64 `json:"ns"` }
func (d *diagnosticHTTP) mark(name string) {
 d.mu.Lock()
 defer d.mu.Unlock()
 if len(d.events) < 64 { d.events = append(d.events, diagnosticHTTPEvent{name, time.Since(d.start).Nanoseconds()}) }
}
func (d *diagnosticHTTP) Read(p []byte) (int, error) {
 n, err := d.body.Read(p)
 d.bytes += int64(n) // Only the ordinary caller reads the body.
 return n, err
}
func (d *diagnosticHTTP) Close() error {
 err := d.body.Close()
 d.mark("body_closed")
 d.mu.Lock()
 defer d.mu.Unlock()
 // Fixed fields only. No addresses, headers, payloads, tokens or error strings.
 b, marshalErr := json.Marshal(struct {
 Type string `json:"type"`
 Kind string `json:"kind"`
 Status int `json:"status"`
 Protocol string `json:"protocol"`
 RequestID string `json:"request_id"`
 Reused bool `json:"reused"`
 Bytes int64 `json:"bytes"`
 Events []diagnosticHTTPEvent `json:"events"`
 }{"diagnostic_http",d.kind,d.status,d.protocol,d.requestID,d.reused,d.bytes,d.events})
 if marshalErr == nil { fmt.Fprintln(os.Stderr,string(b)) } // Best-effort diagnostic, never changes the result.
 return err
}
func diagnosticMark(resp *http.Response, name string) {
 if d, ok := resp.Body.(*diagnosticHTTP); ok { d.mark(name) }
}
func diagnosticRequest(c *http.Client, req *http.Request, kind string) (*http.Response,error) {
 d := &diagnosticHTTP{start:time.Now(),kind:kind}
 d.mark("request_start")
 trace := &httptrace.ClientTrace{
 DNSStart: func(httptrace.DNSStartInfo){ d.mark("dns_start") },
 DNSDone: func(httptrace.DNSDoneInfo){ d.mark("dns_done") },
 ConnectStart: func(string,string){ d.mark("connect_start") },
 ConnectDone: func(_ string,_ string,err error){ if err == nil { d.mark("connect_done") } else { d.mark("connect_failed") } },
 TLSHandshakeStart: func(){ d.mark("tls_start") },
 TLSHandshakeDone: func(_ tls.ConnectionState,err error){ if err == nil { d.mark("tls_done") } else { d.mark("tls_failed") } },
 GotConn: func(info httptrace.GotConnInfo){ d.mu.Lock(); d.reused = info.Reused; d.mu.Unlock(); d.mark("got_connection") },
 WroteRequest: func(httptrace.WroteRequestInfo){ d.mark("request_written") },
 GotFirstResponseByte: func(){ d.mark("first_response_byte") },
 }
 req = req.WithContext(httptrace.WithClientTrace(req.Context(),trace))
 resp,err := c.Do(req)
 d.mark("headers_returned")
 if err != nil { return resp,err }
 d.status = resp.StatusCode
 d.protocol = resp.Proto
 id := resp.Header.Get("X-Receipt-Request-ID")
 if regexp.MustCompile(`^[a-f0-9]{32}-[a-f0-9]+$`).MatchString(id) { d.requestID = id }
 d.body = resp.Body
 resp.Body = d
 return resp,nil
}
