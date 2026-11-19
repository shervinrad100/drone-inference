# video-store

Folder `storage`, compose service `video-store`. The raw video archive in the design: it keeps the
footage for reviewing missed detections and, later, for the offline model-improvement system.

## What it does today

- `main` creates the logger and returns, so the container exits as soon as it starts. Compose maps
  port 8081, but nothing listens on it.
- `StorageDestination.Write` is a placeholder sink that prints the byte count with `fmt.Printf`.
  Nothing calls it yet.

## Running it

No flags yet. `LOG_LEVEL` sets the log level (compose: `WARN`).

## Code

All of it is in `videoStore.go`.

## Open work

- [ ] Start a TCP server and read frames. Compose maps port 8081, but nothing listens on it.
- [ ] Design the API in layers: primitive calls, then low-level, then high-level calls built from
  them.
- [ ] Assemble the frames into a video and save it to a file.
- [ ] `Write` uses `fmt.Printf` for every frame, ignoring `LOG_LEVEL`. Use the logger.
  (`service.name`). The collector routes logs to `logs.<service>` by it.
