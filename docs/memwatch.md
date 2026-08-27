# Memwatch — in-process heap-spike profiler

`services/memwatch` captures pprof profiles at the moment of an abnormal
heap spike, from inside the process.

## Why it exists

The 2026-08 web-ui memory incidents are single requests that allocate
gigabytes and release them faster than the Prometheus scrape interval:
a 5-minute working-set trace shows `0.9Gi → 3.5Gi → 0.9Gi` across three
scrapes. Nothing in logs or metrics identifies the allocation site, and
by the time anyone attaches to the pprof port the heap is normal again.
A watcher inside the process is the only vantage point that reliably
sees the spike.

## How it works

A goroutine (started from `serve.go`, deliberately **not** a
`cs.Servable` — diagnostics must never take the process down) reads
`runtime.MemStats.HeapAlloc` every 2 s. When it crosses the threshold:

1. `heap-<UTC stamp>.pb.gz` and `goroutine-<UTC stamp>.pb.gz` are
   written to the dump directory (the goroutine profile shows which
   handler was running — usually the culprit request);
2. an **error-level** log line `memwatch: heap above threshold` with
   `heap_bytes` is emitted — this is the Loki marker that profiles
   exist;
3. further dumps are suppressed for a 10-minute cooldown, and only the
   newest 3 generations of each profile kind are kept.

Note: the heap profile reflects allocation sites as of the last
completed GC cycle; no forced GC is done (that would worsen the
incident being observed).

## Configuration

| Flag | Env | Default | |
|---|---|---|---|
| `--memwatch-threshold-mb` | `MEMWATCH_THRESHOLD_MB` | `2048` | `0` disables the watcher |
| `--memwatch-dir` | `MEMWATCH_DIR` | `/tmp/memwatch` | dumps live in the container FS |

## Retrieving dumps

```
kubectl exec -n webtor <pod> -- ls /tmp/memwatch
kubectl cp webtor/<pod>:/tmp/memwatch/heap-<stamp>.pb.gz ./heap.pb.gz
go tool pprof -http :0 heap.pb.gz
```

Dumps do not survive a pod restart; the incidents this targets do not
restart the pod (no OOM kill — the limit is not reached), so in
practice the files are there when the Loki marker fires.
