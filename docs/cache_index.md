# Cache index — what Webtor already holds

`public.cache_index` answers one question without touching a torrent: **which
files start at once**. It is what puts the bolt on the Stremio addon's streams
(`EnrichStream`) and, since 2026-09-21, the "Instant" chip on Discover's.

"Cached" for the Webtor backend means: the file is complete in a **seeder** or
stored in the **Vault**. rest-api reports exactly that as `Meta.Cache` (a
`?done=true` probe of our own storage); it does not say which of the two.

## Who writes it

| Source (`source` column) | Written by | Taken back by | Believed for |
|---|---|---|---|
| `probe` (1) | anything that asked the backend and got "cached": `link_resolver.ResolveLink` (Stremio, library), and every **stream and download job on the site** (`jobs/scripts/cache_note.go`) | the same question answered "no" (removes **every** source for that file), or age | `CACHE_INDEX_EXPIRE`, 12 h |
| `seeder` (2) | NATS `resource.cached` from the seeder — the file became complete on its disk | NATS `resource.uncached` from the seeder (piece eviction) or its disk cleaner (whole torrent), or age | `CACHE_INDEX_SEEDER_EXPIRE`, 7 d |

A whole torrent in the Vault is not in this table at all: readers take it from
`vault.resource.vaulted` (`CacheIndex.Lookup`).

### Why the source is part of the key (migration 73)

`source` is a `smallint`; the dictionary is `models.CacheSource` (numbers are
never reused, and start at 1 because go-pg drops zero values from an insert).

One shared row would let a cleaner's "gone from my disk" erase the knowledge
that the same file is in the Vault — per-file Vault storage is known to the
index only through `probe`. So an event can remove only what an event wrote
(`UnmarkFromSeeder`), and only a fresh "no" from the backend itself removes
everything (`Unmark`). Test: `models/cache_index_sql_test.go`.

### Why events, and why the expiry stays

Until 2026-09 the only writer was `ResolveLink`: 13 live entries against ~10k
site starts a week. Marking from the site's jobs fixes the volume but not the
blind spot — a start sees "not cached", the transfer then completes the file,
and nobody asks again. The seeder is the one party that knows the moment, and
its events also cover traffic that never passes web-ui (embeds, the API).

The expiry is the backstop, not the mechanism: a node that dies takes its disk
with it and sends nothing, and after a change of node layout the same torrent
can sit on two nodes (the cleaner of one then un-marks it early). Both failures
are soft — a false mark costs a slow start, a missing one costs nothing.

## Events

JetStream stream `common` (`resource.*`, 24 h), durable pull consumers
`web-ui-resource-cached` / `web-ui-resource-uncached` declared in the web-ui
chart. Handlers: `handlers/event/cached.go`.

```
resource.cached    {"resource_id": "<40 hex, lowercase>", "file_idx": 3}
resource.uncached  {"resource_id": "<40 hex, lowercase>", "file_idx": 3}
resource.uncached  {"resource_id": "<40 hex, lowercase>"}        whole torrent
```

`file_idx` is the file's position in the torrent's file order — the same number
as rest-api's `ListItem.Index` and Stremio's `fileIdx`.

A malformed message is acknowledged and dropped (redelivery cannot repair it);
a database error is returned, so the message is NAKed and comes again.

Unlike the other subscriptions these two **may fail to bind** without taking
the process down — a deployment without the consumers runs as before the
events, and says so once (`cache events are not consumed yet: …`). The bind is
retried every minute: on the rollout that introduces a consumer the pod can
start before the operator has created it.

Publishers: `torrent-web-seeder` (`server/services/cache_events.go`, from the
piece-completion layer — once per transition, not per tick) and
`torrent-web-seeder-cleaner` (`services/cache_events.go`, after a directory is
removed). Both are on when `NATS_SERVICE_HOST` is set, which Kubernetes does by
itself for pods in the namespace of the `nats` service.

## Discover

`POST /discover/availability` (auth), body `{items:[{infoHash, fileIdx?}]}`,
at most 500 items; answers `{cached:[positions]}`. Two indexed reads whatever
the length. A stream that names no file counts as cached when any file of its
torrent is. On an index error the answer is empty, not a 5xx: the chip is a
hint.

Client: `assets/src/js/lib/discover/availabilityClient.js`. The stream list
**waits** for the answer (≤1.5 s) rather than re-sorting under the viewer's
finger; cached streams go first, order otherwise untouched. Umami:
`discover-streams-loaded` carries `cached` (how many of the list were).
