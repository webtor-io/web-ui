# Support form and report confirmation

`handlers/support` serves the help center form (`/support`) and the link of
abuse-store's confirmation letter (`/support/confirm`). abuse-store decides
what a notice does; web-ui collects it, checks what the reporter left out, and
says what happened.

## Causes

The form value is the `PushRequest.Cause` it is sent as; links
(`/support?cause=2&infohash=…` on a torrent page) and old forms post the same
numbers, so they never change. The select lists them in `CauseTypes` order.

| Value | Cause | Needs (checked server-side, `reporterErr` / `needsInfohash`) |
|---|---|---|
| 0 | Copyright infringement (`ILLEGAL_CONTENT`) | a hash in the infohash field, the reporter's name or company, an email, both statements (good faith; accurate and authorized, under penalty of perjury) |
| 5 | Intimate image of me without consent (`INTIMATE_IMAGE`) | a hash, an email; the name is optional |
| 4 | Child sexual abuse material (`CSAM_REPORT`) | a hash; the email is optional, no statements |
| 1 | Malware | a hash |
| 2 | Site error | a hash |
| 3 | Question | nothing |

The browser marks the same fields `required` (`app/support.js`, from
`data-select` / `data-required-select`); the server's check is the one that
counts. A missing field comes back to the form as its own message
(`error.support_name_required`, `error.support_email_required`,
`error.support_statements_required`). The statement checkboxes post `true`:
gin binds a bool with `strconv.ParseBool`, which refuses the browser's default
`on`.

Hash extraction, the 20-hash cap and the scan bounds are unchanged (comments
in `handler.go`). Every notice goes with `Source=FORM`, plus `reporter_name`,
`good_faith` and `authorized` as typed.

## What abuse-store answers

- `confirmation_sent=true`: with `--form-confirm` on, a copyright or intimate
  image notice is held, nothing is blocked, and the reporter gets a letter
  with the link. The success page says the report takes effect only once
  confirmed from that letter, within 72 hours (`support.confirmationSent`).
- `AlreadyExists` (every hash already blocked): the usual success page.
- `ResourceExhausted` (too many notices wait for confirmation, per address or
  in all): `error.support_too_many_pending`.
- Anything else: the form with the error, as before.

A CSAM report is never stored or banned by abuse-store: it mails support
only. Banning goes through its admin port.

## `/support/confirm`

The letter's link is `/support/confirm?t=<token>`. Mail scanners fetch every
link in a message, so the GET only renders a page with a button and calls
nothing; the button POSTs `t` with the session's `_csrf` (the global session
middleware checks it). The POST calls `Confirm` and renders:

| Status | Message |
|---|---|
| `CONFIRMED` | `support.confirm.confirmed` |
| `ALREADY_CONFIRMED` | `support.confirm.alreadyConfirmed` |
| `EXPIRED` | `support.confirm.expired` |
| `UNKNOWN`, or a status this build does not know | `support.confirm.invalid` |
| gRPC error | `error.support_confirm_failed`, with the button kept: the token stays usable |

A token that is empty or longer than 64 characters (abuse-store's are 43)
is an invalid link, 400, on the GET and the POST, and reaches no `Confirm`.

The token confirms the report, so it is kept out of the logs and analytics:
the handler never logs it, `helpers.RedactURL` replaces `t=` on this page in
the access log, and the Umami tracker sends the page as its bare path
(`docs/analytics.md`).
