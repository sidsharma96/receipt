# 22. Explain the slow network hits

A Linux CI process asks Receipt's real server for an existing result. One fresh
fetch takes **391 ms before repository fingerprinting**. The result is only
65,789 bytes. This time we recorded individual reads, TCP packet headers,
server request IDs and CPU counters, then repeated controls on another runner.
We reproduced the slow behavior rather than inferring it from older jobs.

Follow this one request in order: DNS takes 3.4 ms, TCP 65.6 ms, TLS 81.6 ms,
waiting for the first response byte 98.1 ms, collecting the body 131.9 ms and
checking it 9.9 ms. Small gaps complete 391 ms. Its server handling overlaps
waiting/receiving and must not be added again. The usual full CLI also collects
repository inputs; that work is outside this experiment.

![One real request, observed server window growth and a separate reuse control](../assets/network-deep-dive.svg)

Text equivalent: the first run has a roughly 65 ms network round trip. Its fresh
fetch median is 383.4 ms. On a separate roughly 51 ms path, fresh fetches have a
303.3 ms median; an already established connection has a 79.8 ms median. Selected
server socket states show its send window growing from 10 to 20, 40 and 60
segments as delivery is acknowledged. The figure labels the two runs separately.
Reuse is a diagnostic control in a resident process, not an implemented daemon.

Five independent investigation threads reviewed the exact Go client source,
Linux TCP behavior, Caddy and historical records. Their proposals fed two
read-only Linux CI jobs. All 216 fetches returned the same body and decoded
result; 24 additional calls checked shared status. Nothing was executed or
published through the cache. The production service/configuration stayed intact.

## DNS: an answer for IPv6 can hold up IPv4

Before connecting, Go asks for both an IPv4 address, called an A record, and an
IPv6 address, called an AAAA record. The reviewed
[Go 1.22.12 lookup loop](https://github.com/golang/go/blob/go1.22.12/src/net/dnsclient_unix.go#L624-L815)
collects both completions. A quick usable IPv4 answer does not finish that loop
while the IPv6 question remains outstanding.

The second runner uses the local systemd-resolved service at 127.0.0.53. After
the initial cold lookup, IPv4 answers usually return from that local service
quickly: the captured A-query median is **0.17 ms**. Every captured AAAA question
is forwarded upstream. The slowest captured upstream AAAA response takes
**98.57 ms** and says there is no AAAA answer. That is a valid empty result,
not an IPv6 TCP connection failure. Go still needs to receive it.

For two instrumented AAAA reads above 50 ms, Go finishes reading only about
**0.07–0.08 ms after the packet arrives**. In those samples, the missing time
was before packet arrival, not a 100 ms Go scheduling delay. The first cold A
lookup also needed an upstream request and took about 91 ms. This does not make
every historical DNS spike an AAAA problem: the original runs lack those packets.

We do not know why the upstream resolver took that long or why these empty AAAA
responses were forwarded repeatedly instead of answered locally. We have located
the visible delay, not diagnosed the recursive resolver's internals. Changing
DNS cache policy or address selection needs a separate experiment. Permanently
pinning an IP or dropping IPv6 would introduce failover and future-address costs.

## TCP: the connection timer is measuring a real round trip

The client sends a SYN control message, receives SYN-ACK, then continues. In the
first run, the median packet interval is **64.88 ms** and the Go connection span
is about **65.02 ms**. Their per-request difference has a **0.14 ms median**.
The second run has a roughly 51 ms path. These packets strongly support actual
transport/remote-kernel waiting rather than tens of milliseconds of client code.

The old 24 ms and 1.9 ms jobs remain separate observations. Different runner
placement or routing is plausible, but geographic distance was not measured.
Historical records show one successful connection attempt per request, without
evidence that address-family fallback caused those stable baselines. Packet
retransmissions or scheduling can still affect other requests.

## TLS: certificate trust has a per-process startup cost

After TCP, the client establishes encryption and verifies the server's identity.
A fresh Linux Go process loads its trusted certificate authorities from local
files. [System roots initialization](https://github.com/golang/go/blob/go1.22.12/src/crypto/x509/root.go)
is protected by sync.Once: once per process, not once per machine. The
[Unix loader](https://github.com/golang/go/blob/go1.22.12/src/crypto/x509/root_unix.go)
reads certificate bundles/directories and builds an in-memory trust pool.

A standalone root-load control costs about **7.4–9.4 ms elapsed**, allocates
**3.13 MB**, and creates about **31,000 allocations** on these runners. Loading
the same normal trust store before the handshake reduces its measured TLS CPU
and latency. It moves work outside that timer; it does not eliminate the work.
In the 51 ms-path public control, cold TLS is about 64 ms; after preloading it is
about 57 ms. These public 401 requests test TLS, not authenticated cache handling.

Certificate cryptography also matters. Two coarse CPU profiles repeatedly locate
P-384 arithmetic used during certificate verification. We inspected
[the actual second probe's machine code](../measurements/network-deep-dive-2026-10-20/tls-assembly.json):
`MULQ` multiplies integer parts and carry-handling instructions combine the
results. This is computation, not a hidden network sleep. The profiles contain
only 20–24 CPU samples each, so they identify a function family without precise
instruction-level costs. They do not justify replacing cryptographic code.

A warm full handshake still opens TCP and verifies certificates. A resumed TLS
session opens TCP but uses a ticket from an earlier connection. Reusing an
already established connection avoids both setup operations. Our controls verify
actual resumption/reuse flags; they are different mechanisms. None shares state
across independent CLI processes automatically, and verification remains enabled.

## First response: network travel plus real backend work

For each request we match the server-generated request ID to its handler log.
In the 65 ms-path fresh fetch group, the median handler is **27.10 ms**; the
median of each request's first-byte interval minus its own handler is **65.21 ms**.
The much smaller status handler leaves a similar network-sized residual.
The second run gives the same pattern around its 51 ms path.

This residual is not an exact network timer: proxy buffering, scheduling and
handler/write boundaries remain inside it. But the matching and status control
make network travel plus backend work a strong explanation. The backend's
[JSON CPU profile](21-profile-application-cpu.md) explains substantial local work.
Its query, file and lock clocks do not show comparable large waits in these
sequential hits. Caddy can reuse its separate loopback connection to Receipt;
a fresh public client does not inherently require a fresh upstream connection.

Synchronous request logging can delay buffered output after the recorded handler
ends. An isolated blocked-writer experiment demonstrates that possibility, but
these timings do not identify it as the main production cause. SQL indexes,
proxy flush tuning and logging changes need evidence from their own boundaries.

## Reads: fresh TCP delivery waits for acknowledgments

Receipt collects response bytes before checking them. In
[`Remote.Get`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/http.go#L62-L82):

```go
	b, e := io.ReadAll(io.LimitReader(resp.Body, MaxEntryBytes+1))
	if e != nil {
		return v, e
	}
	v, e = Decode(b)
```

ReadAll can wait for more bytes; Decode starts afterward. In the 65 ms-path
fresh group, collecting bytes takes **129.97 ms**, but consumes only about
**1.69 ms of client process CPU**. Checking the result then takes about 10 ms.
On the 51 ms path, body collection takes **101.71 ms**, about two round trips,
with roughly 1.14 ms CPU. JSON checking cannot explain that separate read wait.

TCP limits unacknowledged data through a congestion window, then grows it as
receipts arrive. The matched server kernel shows fresh sockets starting at ten
segments and growing through larger windows during delivery. Packet headers
show spaced bursts. NAT changed the client ports, so we matched the server's
initial SYN-ACK sequence number to its kernel socket state rather than joining
by port alone. All 101 newly established diagnostic connections matched.

Keeping the connection changes this behavior: warmed body-read medians are
**1.08 ms and 0.27 ms** in the two runs, with identical bytes. A fresh HTTP/1.1
control still waits about two round trips, weakening an HTTP/2-specific theory.
Go 1.22's HTTP/2 receive limits also exceed this single response substantially.
Window growth plus the controls strongly support TCP startup/ACK-dependent
delivery. They do not prove every gap comes solely from congestion control;
TLS record boundaries, proxy writes, pacing and delayed ACKs shape the bursts.

## What to focus on next

A fresh fetch pays roughly five round-trip-sized waits: TCP setup, TLS exchange,
request/first-response travel and about two more during body delivery. The local
TLS and validation work then adds to that cost. This accounts for the reproduced
300–383 ms request class without inventing a long application sleep.

The biggest transport opportunities are placing the cache near the actual CI
caller and keeping connections alive across multiple operations. A resident
helper or batching would add lifetime, memory, credential-refresh and failure
rules; neither is implemented. Merely saving http.Client in a local variable
inside a process that exits does not solve it. JSON allocation remains a concrete
CPU target, and DNS's upstream/negative-answer behavior deserves a targeted
resolver investigation. Those are different decisions with different evidence.

The [measurement record](../MEASUREMENTS.md#network-stage-deep-dive-20-october-2026)
and [nine-question review](../PHASE-REVIEWS.md#network-stage-deep-dive-20-october-2026)
retain runs, source versions, observer effects and remaining limits. These are
sequential diagnostics with altered process/connection lifetimes, not a shipped
optimization, capacity test or exact reconstruction of every earlier slow call.

## Check your understanding

<!-- questions:start -->

### Can an empty IPv6 answer delay an IPv4 connection?

**Level:** Newcomer

Go gets an IPv4 address quickly but is still waiting for the IPv6 DNS reply. Can lookup time include that wait even when the eventual connection uses IPv4?

<details>
<summary>Answer</summary>

Yes. The reviewed lookup collects both A and AAAA completions. The AAAA reply may validly contain no address. Waiting for it is different from attempting and failing an IPv6 TCP connection.

**Learn more:** [22. Explain the slow network hits](../guide/22-explain-network-latency.md)

</details>

### Did preloading certificates eliminate their cost?

**Level:** Practitioner

A process loads the normal trust store before starting the TLS timer. TLS becomes faster. Has the process necessarily become equally faster overall?

<details>
<summary>Answer</summary>

No. Loading moved outside the timer. Include its CPU, memory and elapsed time before claiming an overall improvement. A later request in a resident process can reuse initialized state, but a new process has to initialize again.

**Learn more:** [22. Explain the slow network hits](../guide/22-explain-network-latency.md)

</details>

### Why does a port-only packet join fail across NAT?

**Level:** Expert

The runner records one source port, but the server observes another. Does that prove the packet and server socket cannot belong to the same connection?

<details>
<summary>Answer</summary>

No. Network address translation can rewrite the port. This investigation matched the server SYN-ACK initial sequence number plus one to the initial server send state. The match identifies a connection; a reused connection still needs request IDs and boundaries to distinguish its requests.

**Learn more:** [22. Explain the slow network hits](../guide/22-explain-network-latency.md)

</details>

<!-- questions:end -->
