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
- a late client gets the full history

and also define how we want to handle dropped chunks... not sure.
