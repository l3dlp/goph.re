# TODO — Gophre ideas

Focus: turn Gophre from a raw feed into something that *digests* Go news for you,
using a cheap LLM (e.g. `gemini-3.8-flash`) so the whole pipeline costs cents per month.

## 1. Article enrichment pass (`gophre digest`)

A new CLI command that runs right after `gophre update` in the hourly cron. It scans
`rss_posts.json` for articles that have not been digested yet and sends them to the
LLM in batches.

- [ ] Add fields to `data.Article`: `Summary string`, `Tags []string`, `Score int`,
      `Digested bool` (omitempty so old entries stay valid).
- [ ] New `pkg/llm` package: one plain-HTTP client for the Gemini API (no SDK needed —
      one POST endpoint), key in `.env` as `GEMINI_API_KEY`. It will automatically go
      through the IPv4-only transport.
- [ ] New `pkg/service/digest.go`: batch 20–30 articles per request, send only
      title + first ~300 chars of the (bluemonday-stripped) description, ask for JSON
      output: `[{id, summary, tags, score}]`.
- [ ] Per article: a 1–2 sentence TL;DR in plain language, 2–4 topic tags
      (e.g. `generics`, `performance`, `release`, `tooling`, `opinion`), and a 0–10
      "is this actually about Go / is it substantial" score to bury spam and off-topic
      posts without deleting them.
- [ ] Idempotent + cheap: never re-digest (`Digested == true` → skip), cap each run
      (e.g. 100 articles) so a backfill doesn't burn quota, mark failures and retry
      next hour.
- [ ] `update.sh` becomes: `gophre update && gophre digest && gigit "Hourly update"`.

## 2. One digest message instead of Discord spam

Today every new article is posted to Discord individually. Replace (or complement)
that with a digest:

- [ ] After enrichment, post a single message per run: "**7 new Go articles** — top 3
      worth reading:" with the LLM-written one-liners, links, and a collapsed count of
      the rest. Skip the message entirely when nothing scored above a threshold.
- [ ] Optional daily mode: `gophre digest --daily` summarizes the last 24h into one
      short editorial paragraph ("What happened in Go today") — flash-class models are
      good at this and one call/day is basically free.

## 3. Personal ranking from existing votes

`votes.json` + `users/<id>.json` are free training data: they say what you keep and
what you reject.

- [ ] Build a tiny "taste profile" prompt: titles/tags of your last ~30 GOOD and ~30
      BAD votes, then ask the LLM to score each new article 0–10 "would this user keep
      it?". Store as `PersonalScore` (per-user file, not in the shared posts file).
- [ ] `/for-you` page: the normal wall, ordered by predicted score.
- [ ] Cheaper variant to try first: no LLM at runtime at all — count tag frequencies
      in GOOD vs BAD votes and rank by tag affinity. The LLM already produced the tags
      in step 1, so this is a pure Go loop.

## 4. Story clustering / dedup

Big Go news (a release, a CVE) appears in 5+ feeds at once.

- [ ] In the digest batch, ask the LLM to group same-story articles (`cluster_id`),
      show one card per cluster on the wall with "also covered by …" links.

## 5. Weekly roundup

- [ ] `gophre digest --weekly`: feed the week's top-voted + top-scored articles to the
      LLM and generate a markdown roundup ("This week in Go"). Serve at `/weekly`
      (and it doubles as newsletter content if you ever want email).

## 6. Search that understands topics

- [ ] `/search` currently does `strings.Contains` on title/description. Once tags
      exist, match query terms against tags too (`q=testing` finds articles tagged
      `testing` even when the title says "table-driven patterns"). No embeddings, no
      vector DB — tags from step 1 are enough at this scale.

## Cost guardrails (why flash-class is enough)

- Volume is tiny: an hourly run sees a handful of new articles; batched, that's a few
  thousand input tokens per call, and flash-tier pricing makes that < $1/month.
- Send titles + trimmed descriptions only, never full pages; request strict JSON with
  short summaries (`maxOutputTokens` low).
- Everything is cached in the JSON files by article ID — an article is paid for once
  in its lifetime.
- Graceful degradation: if the API is down or the key is missing, `digest` logs and
  exits 0; the site keeps working exactly as today.

## Housekeeping (not LLM, but worth doing)

- [x] Fix `pkg/web/auth_test.go`: it uses `UserID` but `data.User`'s field is `ID`,
      so `go test ./pkg/web/` doesn't compile. (Also fixed the `RequireAdmin` debug
      leftover that printed the user ID instead of redirecting non-admins.)
- [x] `pkg/rss/topic.go` sanitizes the wrong slice (`articles[idx]` instead of
      `goodArticles[idx]`), so topic pages return unsanitized descriptions.
- [x] Unify the two vote paths: the frontend only called `POST /vote/:id/:vote`
      (the legacy calls were commented out), so the legacy `POST /vote?url=` route
      and `rss.UpdateVoteByURL` are removed.
- [x] Move the session cookie key (`"secret-session-key"` in `www.go`) into `.env`
      as `SESSION_KEY` (random per-start fallback when unset).
- [x] The per-IP rate-limiter map grows forever; evict idle entries. (Janitor
      goroutine evicts visitors idle > 15 min; also fixed a create-race in
      `GetLimiter`.)
