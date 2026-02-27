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
