# learn how to broadcast

we're gonna make up a fake example that should cover a simplified version of the jobs
so i can learn all the things / gain intuition. but i dont want to layer on tls and grpc
and such.

to cover:
- "per station" broadcaster pattern
- broadcasts to potentially many listeners
- all listeners get the same output, from the start of broadcast
- no polling, no busy-waiting
- not just text, should handle binary
- assume in-memory state is fine

routes look like:

POST /station                    creates a station, returns ID
POST /station/broadcast?id=rock  DJ sends a track
GET  /station/listen?id=rock     tune in, stream output
DELETE /station?id=rock          shut down the station

scenarios to cover w tests:
- works with binary + strings
- slow reader
- server drops mid-stream
- cleanup happens upon disconnect
- no deadlocks/blocking, no goroutine leaks (goleak)
- we can scale to <some large number> of connections
- a late client gets the full history [how big can history get?]

and also define how we want to handle dropped chunks... not sure.
not because of slow reader because of network issues for example.
ignore?

# curl statements for basic functional testing

```
# create a named station
curl -X POST "localhost:8080/station?id=punk"

# DJ sends data
curl -X POST -d "Now playing: Time Bomb" "localhost:8080/station/broadcast?id=punk"
curl -X POST --data-binary @timebomb.mp3 "localhost:8080/station/broadcast?id=punk"

# shut down station
curl -X DELETE "localhost:8080/station?id=punk"

# you can't listen to a nonexistant station
curl localhost:8080/station/listen?id=nope # 404

# open multiple listeners at once
curl localhost:8080/station/listen?id=rock &
curl localhost:8080/station/listen?id=rock &
curl -X POST -d "hello vietnam" "localhost:8080/station/broadcast?id=rock"
```

# explaining the concurrency so an idiot (namely me) can understand it

A single goroutine (see NewBroadcaster) owns all the mutable state - the subscriber map and the history buffer.

The public methods (Subscribe, Unsubscribe, Send, Close) are just thin wrapper around message sending and reading to the appropriate channels. The monitor processes these one at a time in a select loop - that's what serializes access to the state.

I use a mutex to protect the station map thats touched by goroutines calling Create, Get, Stop concurrently.


# concerns

for close...
return exits the run() function, which kills the monitor goroutine.
The channels on the Broadcaster struct (subscribeCh, sendCh, etc.) become orphaned — nobody's reading from them anymore. Any future calls to Subscribe, Send, etc. would block forever.



# debugging

finally i am annoyed enough at the subscription disconnect test i am going to use
this instead of a print statement

go install github.com/go-delve/delve/cmd/dlv@latest

dlv test -- -test.run TestCleanupOnDisconnect -test.timeout 10s

// set the breakpoint to r.Context().Done()
break main.go:93
continue

// hangs... ctrl c

> [unrecovered-panic] runtime.fatalpanic() /opt/homebrew/opt/go/libexec/src/runtime/panic.go:1298 (hits goroutine(29):1 total:1) (PC: 0x104d90d50)


goroutine 29
bt


// run without optimized binary??
// nope doesnt work

dlv test -- -test.run TestCleanupOnDisconnect

// no... let's go easy again
go test -v -run TestCleanupOnDisconnect -timeout 10s 2>&1

// fails here:
dj-broadcast.TestCleanupOnDisconnect(0x14000100700)
        /Users/pk/golang-learning/dj-broadcast/main_test.go:193 +0xd4


# goleaks
we find leaks in the tests but not the app. what to do?


# adding a pantload of CI/CD esp. to spot issues i've missed

## local
brew install golangci-lint
brew install semgrep

```
make setup-hooks
make ci
# runs: fmt-check → vet → lint → semgrep → race (count=10)

# run individual pieces
make test          # plain go test
make race          # go test -race -count=10
make lint          # golangci-lint
make semgrep       # custom rules + p/golang + p/trailofbits
make gcatch        # GCatch (needs Z3 + GCatch installed locally)

# run stress tests (slow — this is the count=100 suite)
make stress

# run everything
  make ci-full       # ci + stress
```




# behind the filtering of ~/golang-learning/snippetbox/cmd/server/testmain_test.go

https://github.com/uber-go/goleak/discussions/89

https://github.com/uber-go/goleak/blob/2b7fd8a0d244fa0d8b5857330fd1cefce940fa53/options.go#L65



# looking at errors

make ci                                                                                             ()
go vet ./...
golangci-lint run ./...
main_test.go:70:23: response body must be closed (bodyclose)
        resp, err := http.Get(srv.URL + "/station/listen?id=punk")
                             ^
main.go:12:21: Error return value of `http.ListenAndServe` is not checked (errcheck)
        http.ListenAndServe(":8080", srv)
                           ^
main.go:42:15: Error return value of `fmt.Fprintln` is not checked (errcheck)
                fmt.Fprintln(w, "station created:", id)
                            ^
main.go:59:15: Error return value of `fmt.Fprintln` is not checked (errcheck)
                fmt.Fprintln(w, "station deleted:", id)
                            ^
main.go:92:12: Error return value of `w.Write` is not checked (errcheck)
                                w.Write(msg)
                                       ^
broadcaster.go:152:1: unnamedResult: consider giving a name to these results (gocritic)
func (b *Broadcaster) Subscribe() (int, chan []byte) {
^
broadcaster.go:70:2: field history is unused (unused)
        history [][]byte
        ^
7 issues:
* bodyclose: 1
* errcheck: 4
* gocritic: 1
* unused: 1
make: *** [lint] Error 1


## after linting fixed
  Semgrep findings:

  1. unbounded-append-in-loop — real finding, history grows forever. Already in your stress test TODO.
  2. channel-close-without-once — lines 122 and 141 in broadcaster.go. Line 122 is close(ch) inside Unsubscribe (single subscriber
  channel), line 141 is close(ch) in the Close loop. Both are inside run() which is single-goroutine so double-close can't happen
  there. But your stress test already proved the outer Close() method has a double-close problem. These are partially false positives
  since the closes are inside the monitor goroutine.
  3. use-tls — http.ListenAndServe without TLS. This is a dev server, suppress or upgrade later.
  4. XSS on w.Write(msg) — line 93 writes raw broadcast bytes to the response. This is an audio/data stream, not an HTML page. False
  positive for your use case. Suppress with // wh: go.net.xss.no-direct-write-to-responsewriter-taint.


line 171-175:
Close() sends a value into closeCh. When run() receives it, it
  returns — killing the goroutine. If anything calls Close() a second time, the send on line 174 blocks forever because nobody is
  receiving from closeCh anymore. That's a goroutine leak.
