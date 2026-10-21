// Linux-only diagnostic program, copied under .tools/ before building.
package main

import (
 "context"
 "crypto/sha256"
 "crypto/tls"
 "crypto/x509"
 "encoding/hex"
 "encoding/json"
 "flag"
 "hash"
 "io"
 "net"
 "net/http"
 "net/http/httptrace"
 "os"
 "github.com/sidsharma96/receipt/internal/cache"
 "regexp"
 "sync"
 "syscall"
 "time"
 "unsafe"
)

type event struct { Name string `json:"name"`; NS int64 `json:"ns"`; CPUUS int64 `json:"cpu_us"`; Bytes int `json:"bytes,omitempty"`; RTTUS uint32 `json:"rtt_us,omitempty"`; Retrans uint32 `json:"total_retrans,omitempty"`; Cwnd uint32 `json:"snd_cwnd,omitempty"` }
type capture struct {
 mu sync.Mutex
 start time.Time
 cpu int64
 Events []event `json:"events"`
 Status int `json:"status"`
 Protocol string `json:"protocol"`
 RequestID string `json:"request_id"`
 Reused bool `json:"reused"`
 Resumed bool `json:"resumed"`
 TLSVersion uint16 `json:"tls_version"`
 Cipher uint16 `json:"cipher"`
 Chain int `json:"certificate_count"`
 BodyBytes int `json:"body_bytes"`
 BodyHash string `json:"body_sha256"`
 conn *net.TCPConn
}
func cpuUS() int64 { var r syscall.Rusage; if syscall.Getrusage(syscall.RUSAGE_SELF,&r)!=nil { return -1 };return r.Utime.Sec*1000000+r.Utime.Usec+r.Stime.Sec*1000000+r.Stime.Usec }
func (c *capture) mark(name string,n int) {
 e:=event{Name:name,NS:time.Since(c.start).Nanoseconds(),CPUUS:cpuUS()-c.cpu,Bytes:n}
 c.mu.Lock();defer c.mu.Unlock()
 if len(c.Events)<512 {c.Events=append(c.Events,e)}
}
func (c *capture) tcp(name string) {
 c.mu.Lock();conn:=c.conn;c.mu.Unlock();if conn==nil{return}
 raw,err:=conn.SyscallConn();if err!=nil{return}
 var value syscall.TCPInfo
 size:=uint32(unsafe.Sizeof(value));var socketErr syscall.Errno
 if raw.Control(func(fd uintptr){_,_,socketErr=syscall.Syscall6(syscall.SYS_GETSOCKOPT,fd,syscall.IPPROTO_TCP,syscall.TCP_INFO,uintptr(unsafe.Pointer(&value)),uintptr(unsafe.Pointer(&size)),0)})!=nil || socketErr!=0{return}
 info:=&value
 c.mu.Lock();defer c.mu.Unlock()
 c.Events=append(c.Events,event{Name:name,NS:time.Since(c.start).Nanoseconds(),CPUUS:cpuUS()-c.cpu,RTTUS:info.Rtt,Retrans:info.Total_retrans,Cwnd:info.Snd_cwnd})
}
type observedConn struct { *net.TCPConn; c *capture }
func (c observedConn) Read(p []byte)(int,error){c.c.mark("socket_read_start",0);n,err:=c.TCPConn.Read(p);c.c.mark("socket_read_done",n);return n,err}
type body struct { io.ReadCloser; c *capture; digest hash.Hash }
func (b *body) Read(p []byte)(int,error){b.c.mark("body_read_start",0);n,err:=b.ReadCloser.Read(p);_,_=b.digest.Write(p[:n]);b.c.BodyBytes+=n;b.c.mark("body_read_done",n);return n,err}
func (b *body) Close()error{b.c.mark("body_close_start",0);b.c.tcp("tcp_after_body");err:=b.ReadCloser.Close();b.c.BodyHash=hex.EncodeToString(b.digest.Sum(nil));b.c.mark("body_close_done",0);return err}
type transport struct { base *http.Transport; current *capture; mode string }
var requestIDPattern=regexp.MustCompile(`^[a-f0-9]{32}-[a-f0-9]{1,16}$`)
func (t *transport) RoundTrip(req *http.Request)(*http.Response,error){
 c:=t.current
 req.Header.Set("Accept-Encoding","identity")
 trace:=&httptrace.ClientTrace{
  DNSStart:func(httptrace.DNSStartInfo){c.mark("dns_start",0)},DNSDone:func(httptrace.DNSDoneInfo){c.mark("dns_done",0)},
  ConnectStart:func(string,string){c.mark("connect_start",0)},ConnectDone:func(_ string,_ string,err error){if err==nil{c.mark("connect_done",0)}else{c.mark("connect_failed",0)}},
  TLSHandshakeStart:func(){c.mark("tls_start",0)},TLSHandshakeDone:func(s tls.ConnectionState,err error){if err==nil{c.Resumed=s.DidResume;c.TLSVersion=s.Version;c.Cipher=s.CipherSuite;c.Chain=len(s.PeerCertificates);c.mark("tls_done",0)}else{c.mark("tls_failed",0)}},
  GotConn:func(i httptrace.GotConnInfo){c.Reused=i.Reused;if tc,ok:=i.Conn.(*tls.Conn);ok{if observed,ok:=tc.NetConn().(observedConn);ok{c.mu.Lock();c.conn=observed.TCPConn;c.mu.Unlock()}};c.mark("got_connection",0);c.tcp("tcp_connected")},
  WroteRequest:func(httptrace.WroteRequestInfo){c.mark("request_written",0)},GotFirstResponseByte:func(){c.mark("first_response_byte",0)},
 }
 resp,err:=t.base.RoundTrip(req.WithContext(httptrace.WithClientTrace(req.Context(),trace)))
 c.mark("headers_returned",0);if err!=nil{return resp,err}
 c.Status=resp.StatusCode;c.Protocol=resp.Proto
 if id:=resp.Header.Get("X-Receipt-Request-ID");requestIDPattern.MatchString(id){c.RequestID=id}
 resp.Body=&body{ReadCloser:resp.Body,c:c,digest:sha256.New()};return resp,nil
}
func main(){
 h1:=flag.Bool("h1",false,"HTTP/1.1 control")
 mode:=flag.String("mode","fresh","fresh, preload, resume, reuse or pinned")
 count:=flag.Int("count",1,"sequential requests")
 endpoint:=flag.String("endpoint","fetch","fetch or status")
 key:=flag.String("key","","existing fixture key")
 flag.Parse()
 if *mode!="fresh"&&*mode!="preload"&&*mode!="resume"&&*mode!="reuse"&&*mode!="pinned" {os.Exit(2)}
 if *endpoint!="fetch"&&*endpoint!="status" {os.Exit(2)}
 if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(*key)||*count<1||*count>100 {os.Exit(2)}
 token:=os.Getenv("RECEIPT_SELF_CI_TOKEN");if token==""{os.Exit(2)}
 rootsCPU:=cpuUS();rootsStart:=time.Now();var roots *x509.CertPool
 if *mode=="preload" {var err error;roots,err=x509.SystemCertPool();if err!=nil{os.Exit(3)}}
 rootMS:=float64(time.Since(rootsStart).Nanoseconds())/1e6;rootCPU:=cpuUS()-rootsCPU
 session:=tls.NewLRUClientSessionCache(8)
 t:=&transport{mode:*mode}
 makeTransport:=func()*http.Transport{
  tr:=http.DefaultTransport.(*http.Transport).Clone()
  tr.ForceAttemptHTTP2=!*h1
  if *h1 {tr.TLSNextProto=map[string]func(string,*tls.Conn)http.RoundTripper{}}
  tr.DialContext=func(ctx context.Context,network,address string)(net.Conn,error){
   if *mode=="pinned" {_,port,err:=net.SplitHostPort(address);if err!=nil{return nil,err};address=net.JoinHostPort("104.248.183.112",port)}
   d:=net.Dialer{Timeout:5*time.Second,KeepAlive:30*time.Second}
   conn,err:=d.DialContext(ctx,network,address);if err!=nil{return nil,err}
   tcp,ok:=conn.(*net.TCPConn);if !ok{return conn,nil};c:=t.current;c.mu.Lock();c.conn=tcp;c.mu.Unlock();return observedConn{tcp,c},nil
  }
  tr.TLSClientConfig=&tls.Config{RootCAs:roots}
  if *mode=="resume" {tr.TLSClientConfig.ClientSessionCache=session}
  return tr
 }
 t.base=makeTransport()
 client:=&http.Client{Transport:t,Timeout:15*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
 remote:=cache.Remote{URL:"https://receipt.sidsharma.dev",Token:token,Client:client}
 for i:=0;i<*count;i++ {
  if i>0 && *mode!="reuse" {t.base.CloseIdleConnections();t.base=makeTransport()}
  c:=&capture{start:time.Now(),cpu:cpuUS()};t.current=c;c.mark("operation_start",0)
  var err error;var decodedHash string
  if *endpoint=="status" {err=remote.Check(context.Background(),*key)} else {
   var value cache.Result;value,err=remote.Get(context.Background(),*key)
   c.mark("operation_done",0)
   if err==nil{var encoded []byte;encoded,err=cache.Encode(value);if err==nil{sum:=sha256.Sum256(encoded);decodedHash=hex.EncodeToString(sum[:])}}
  }
  if *endpoint=="status" {c.mark("operation_done",0)}
  result:=struct{Mode string `json:"mode"`;Index int `json:"index"`;Endpoint string `json:"endpoint"`;StartUnixNS int64 `json:"start_unix_ns"`;RootLoadMS float64 `json:"root_load_ms"`;RootCPUUS int64 `json:"root_cpu_us"`;Failed bool `json:"failed"`;DecodedHash string `json:"decoded_sha256,omitempty"`;Trace *capture `json:"trace"`}{*mode,i,*endpoint,c.start.UnixNano(),rootMS,rootCPU,err!=nil,decodedHash,c}
  c.mu.Lock();encoded,marshalErr:=json.Marshal(result);c.mu.Unlock();if marshalErr!=nil{os.Exit(4)};if _,writeErr:=os.Stdout.Write(append(encoded,'\n'));writeErr!=nil{os.Exit(4)}
  if err!=nil{os.Exit(1)}
  time.Sleep(20*time.Millisecond)
 }
 t.base.CloseIdleConnections()
}
