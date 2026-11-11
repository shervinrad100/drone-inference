# video-feed

The drone's camera: stage 1 (video capture) in the design. It makes frames and sends them over TCP
to the [receiver](../receiver/README.md), through a simulated bad network.

In the design the drone has no logging infrastructure. The frame ID and send time travel in each
frame's header, and the receiver does all the measuring. This service still logs to stdout for local
debugging, but the logging pipeline won't collect it: a real drone's stdout can't be reached from
the ground.

## How it works

- **Camera** (`cameraCapture`): ticks at 60 fps. Each tick makes a frame of 1 KiB random bytes with
  the next ID and the current time, and puts it on a channel that holds 30 frames. When the channel
  is full, the frame is dropped. Its ID was still used, so the receiver sees a gap.
- **Network chaos** (`sendVideoBytes`): reads frames off the channel. 20% of frames hit a fault
  first: half are dropped, and half are delayed by a random 0–3.6 s. The delay sleeps inside the send
  loop, so it holds up every frame behind it.
- **Send:** each frame that gets through is encoded with `video.EncodeFrame` (see the
  [frame protocol](../../../README.md#frame-protocol)) and written to the connection.

## Running it

| Flag | What it sets | Default | In compose |
|---|---|---|---|
| `-ip` | The receiver's host | none | `receiver` |
| `-port` | The receiver's port | none | `8080` |

`LOG_LEVEL` sets the log level (compose: `DEBUG`).

It connects once at startup. If the receiver isn't listening yet, or the connection drops, it exits.
Restart it with `docker compose restart video-feed`.

## Code

All of it is in `video-feed.go`: `main` dials the receiver, `cameraCapture` makes the frames, and
`sendVideoBytes` injects the faults and sends.

## Open work

- [ ] **The chaos caps throughput at about 5 fps.** 10% of frames sleep for 1.8 s on average inside
  the send loop, so each frame costs about 0.18 s and the sender manages about 5.5 frames a second.
  The 30-frame channel stays full, and most of the 60 fps are dropped at the camera. Decide which
  network to model: a stall that blocks everything behind it (today) or per-frame jitter.
- [ ] Startup race: `depends_on` doesn't wait for the receiver to listen, and video-feed exits if the
  dial fails or the connection drops. Reconnect with backoff, or add a compose healthcheck with
  `condition: service_healthy`.
- [ ] Fault injection is mixed into the business logic: `sendVideoBytes` sends frames *and*
  simulates the bad network. Move the chaos into a writer that wraps any `io.Writer`
  (`internal/chaos`).
- [ ] Put the drone video capture on another Docker network.
- [ ] Make it UDP.
- [ ] Graceful shutdown: stop the camera loop with a `context.Context` from `signal.NotifyContext`.
- [ ] Pass the service name to `platform.NewLogger`, so every log line says which service wrote it
  (`service.name`).
- [ ] Loggers are used two ways: `main` uses its own `logger`, and `cameraCapture` uses the global
  `slog`. Pick one.
- [ ] Leftovers: the `HEADER_SIZE` constant duplicates `internal/video`; `FRAME_BUFFER_SIZE` is the
  payload size, not a buffer; the channel size `30` is a magic number. Constants are
  `SCREAMING_CASE`, and Go uses `MixedCaps`.
