
func TestDiagnosticHTTPReadAndReuse(t *testing.T) {
 payload, err := Encode(example())
 if err != nil { t.Fatal(err) }
 server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  if r.Header.Get("Authorization") != "Bearer private-token" { t.Error("authorization changed") }
  w.Header().Set("X-Receipt-Request-ID","untrusted-private-value")
  if _, err := w.Write(payload); err != nil { t.Error(err) }
 }))
 defer server.Close()
 client := server.Client()
 for i:=0;i<2;i++ {
  req, err := http.NewRequestWithContext(context.Background(),http.MethodGet,server.URL,nil)
  if err != nil { t.Fatal(err) }
  req.Header.Set("Authorization","Bearer private-token")
  resp, err := diagnosticRequest(client,req,entriesPath)
  if err != nil { t.Fatal(err) }
  d := resp.Body.(*diagnosticHTTP)
  b, err := io.ReadAll(resp.Body)
  if err != nil { t.Fatal(err) }
  if err := resp.Body.Close(); err != nil { t.Fatal(err) }
  if !bytes.Equal(b,payload) || d.bytes != int64(len(payload)) { t.Fatal("body changed") }
  if d.requestID != "" { t.Fatal("unsafe response ID retained") }
  if i == 1 && !d.reused { t.Fatal("overlay prevented reuse") }
 }
 remote := Remote{URL:server.URL,Token:"private-token",Client:client}
 result,err := remote.Get(context.Background(),example().Key)
 if err != nil { t.Fatal(err) }
 actual,err := Encode(result)
 if err != nil || !bytes.Equal(actual,payload) { t.Fatal("result changed") }
}

func TestDiagnosticHTTPBoundedConcurrentEvents(t *testing.T) {
 d := &diagnosticHTTP{start:time.Now()}
 var wg sync.WaitGroup
 for i:=0;i<128;i++ { wg.Add(1); go func(){ defer wg.Done(); d.mark("connect_start") }() }
 wg.Wait()
 if len(d.events) != 64 { t.Fatal("unbounded or lost event storage") }
}
