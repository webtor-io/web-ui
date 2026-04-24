# User Subtitles

User-uploaded subtitle tracks for video files inside a torrent. Available on
the main site (not embed), authenticated users only.

## Flow

1. While streaming video, the user opens the subtitles modal and clicks
   **My Subtitles**.
2. If unauthenticated: a sign-in CTA is shown with `return-url` pointing at
   the current page.
3. If authenticated: drag-and-drop or file-picker submits a standard POST
   multipart form to `/user-subtitle`.
4. The server hashes the file (SHA-256), stores the raw blob in S3 under
   the hash, and inserts a binding row.
5. The next player render picks up the new track. The track appears under
   its original filename via the `UserSubtitle` provider and is wrapped
   through `torrent-http-proxy`'s `/ext/` → `~vtt/` chain so SRT/ASS
   content is converted to VTT on the fly (same path used for
   OpenSubtitles).

## Storage

- **Blob:** S3, key = SHA-256 hex of the file content. Upload is
  idempotent — re-uploading the same content is a no-op. Bucket is
  configured via `AWS_USER_SUBTITLE_BUCKET` (separate from the poster
  cache bucket).
- **Binding:** PostgreSQL table `public.user_subtitle`. One row per
  `(user_id, resource_id, path, hash)`; that composite is the uniqueness
  constraint. The binding carries the original filename, format, size and
  timestamps.

Dedup is shared across users: if two users upload the same file, there is
one S3 object and two binding rows. No separate refcount table — deletion
serialises on a `pg_advisory_xact_lock(hash)` and removes the S3 object
only after confirming no remaining rows reference the hash.

## Limits

- `10` subtitles per `(user_id, resource_id, path)`. Exceeded uploads
  return `error.user_subtitle.limit_reached`.
- `5 MB` max file size.
- Formats: `srt`, `vtt`, `ass`. Detection uses the file extension first
  with a `WEBVTT` magic-bytes sniff fallback.

## Schema

```sql
CREATE TABLE public.user_subtitle (
    user_subtitle_id    uuid        NOT NULL DEFAULT uuid_generate_v4(),
    user_id             uuid        NOT NULL,
    resource_id         text        NOT NULL,
    path                text        NOT NULL,
    hash                text        NOT NULL,
    original_name       text        NOT NULL,
    format              text        NOT NULL,
    size                bigint      NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT user_subtitle_pk PRIMARY KEY (user_subtitle_id),
    CONSTRAINT user_subtitle_unique UNIQUE (user_id, resource_id, path, hash),
    CONSTRAINT user_subtitle_user_fk FOREIGN KEY (user_id)
        REFERENCES public."user"(user_id) ON DELETE CASCADE
);

CREATE INDEX user_subtitle_lookup_idx ON public.user_subtitle (user_id, resource_id, path);
CREATE INDEX user_subtitle_hash_idx   ON public.user_subtitle (hash);
```

Migration: `48_create_user_subtitle.up.sql`.

## Endpoints

| Method | URL                                  | Auth       | Notes                                                                                           |
|--------|--------------------------------------|------------|-------------------------------------------------------------------------------------------------|
| POST   | `/user-subtitle`                     | required   | multipart: `file`, `resource_id`, `path`, hidden `return_url`. Redirects on success/error.      |
| POST   | `/user-subtitle/delete/:id`          | required   | Decrements the hash refcount and drops the S3 object when the count falls to zero.              |
| GET    | `/user-subtitle/file/:hash/*name`    | **public** | Streams the raw blob. Reached from `torrent-http-proxy` via `/ext/` when the player fetches.    |

The file endpoint must be public because `torrent-http-proxy` (and the
external-proxy service) fetch it anonymously from outside the user's
browser session; the hash makes the URL unguessable — same trust model as
OpenSubtitles URLs.

## Concurrency

Upload and Delete serialise on `pg_advisory_xact_lock(fnv64(hash))` inside
a single `RunInTransaction`. Combined with the always-idempotent
`PutObject`, this eliminates the classic race: if a Delete decides to
purge the object right when an Upload for the same content is starting,
one waits on the lock and the other re-uploads on its next pass, so the
blob is never missing for a live binding.

## Configuration

- `AWS_USER_SUBTITLE_BUCKET` — required. When empty the service is
  disabled: handlers skip registration and the UI hides the **My
  Subtitles** tab.
- Reuses the shared `AWS_*` credentials + endpoint + region from the
  common S3 client.

Chart plumbing: `values.aws.buckets.userSubtitle` → helper `_helpers.tpl`
injects the env var. The production bucket is set in
`web-ui.yaml.gotmpl`.

## Code pointers

- Migration: `migrations/48_create_user_subtitle.up.sql`
- Model: `models/user_subtitle.go`
- Service: `services/user_subtitle/service.go`
- Handler: `handlers/user_subtitle/handler.go`
- Player wiring: `jobs/scripts/action.go` (loads `UserSubtitles` into
  `StreamContent` for the template)
- Template helper: `handlers/action/helper.go` — `GetSubtitles` includes
  the `UserSubtitle` provider in the list that produces `<track>` tags
- UI partial: `templates/partials/action/user_subtitles.html`
- Client JS (drag-and-drop): `assets/src/js/app/action/stream.js`
