# To do

Ideas and requests, with what's done ticked. `FEATURES.md` describes each
feature once it's built.

## Requests from Rob (2026-10-29)

Built on 2026-10-29 and merged into `master` on 2026-10-30. Not deployed yet.

- [x] **Sign up with email, no Google.** Google sign-in is removed. `/login`
  emails a link plus a 6-digit code, valid for 15 minutes. New players pick a
  display name and opt in to news on `/welcome`. Existing users keep their
  account and sign in with the same email.
- [x] **Email through Amazon SES.** Outbox table with retries, per-category
  settings on Settings, one-click unsubscribe. Every email is planned in
  `docs/emails.md`.
- [x] **Trade and game emails:** orders filled, cancelled or expired,
  dividends, splits, game started, ending in 3 days and 1 day, final results,
  next round ready and referral bonus.
- [x] **Newsletters from Rob.** `/admin/email`: write, send a test to
  yourself, then send to everyone who opted in to news.
- [x] **Ask a Question.** `/questions`: members post and vote; an admin
  answers, and the asker and voters get the answer by email. Admins can hide
  or delete questions.
- [x] **Courses.** `/courses`: video lessons from YouTube, Vimeo or Bunny
  Stream, with progress per player. Admins build them at `/admin/courses` and
  can email members when a course is first published.
- [x] **Review the whole game for improvements.** Findings are in the next
  section.
- [ ] **Go live.** Needs an SES IAM user and `SES_ACCESS_KEY_ID`,
  `SES_SECRET_ACCESS_KEY`, `SES_REGION` and `MAIL_FROM` in the server `.env`.
  Without them nobody can sign in. Remove `GOOGLE_CLIENT_*` from the `.env`.
- [ ] **Answer Rob's open questions for email:** sender name and postal address
  for newsletters (`NEWS_SENDER`), reply-to address (`NEWS_REPLY_TO`). The
  newsletter page won't send without `NEWS_SENDER`. See `docs/emails.md`.
- [ ] **Weekly summary and "we miss you" emails** (D1 and N2 in
  `docs/emails.md`). The Weekly summary setting is hidden until D1 exists.
- [ ] **Remove Google leftovers:** `users.picture_url`, the
  `*.googleusercontent.com` entry in the CSP `img-src`, and "Use Google Photo"
  on the Profile page.

## One game per quarter (2026-10-30)

Built on 2026-10-30. Not deployed yet.

- [x] **One game at a time, one each quarter.** Starts at midnight on 1 January,
  April, July and October and ends at midnight at the end of the quarter. The
  scheduler creates this quarter's game and the next one, copying settings
  forward. Players can't create games; templates and recurring games are gone.
- [x] **Past games** are listed on `/games` with their final standings.
- [x] **Only admins change game settings**, at `/admin/games`.
- [ ] **Cancel "Test 3" in production** (`/admin/games/3`) once this is
  deployed, so the quarterly game is the only one running. It has one player
  and no trades.
- [ ] **Email everyone when a quarter's game opens**, not only players who
  joined early. Today G1 (game started) goes only to players already in it.

## From the whole-game review (2026-10-29)

Highest impact for the effort first.

- [x] **Recurring games made a new round every minute** once a round finished.
  Fixed, with a test. No recurring game had finished in production, so no one
  was affected.
- [ ] **Invite links.** "Generate Invite Link" never shows the link
  (`ListGameInvites` is unused). Show it with a copy button, and send a
  signed-out visitor back to `/invite/{code}` after sign-in. `InviteJoin`
  ignores `expires_at` and `max_participants` and joins on a GET.
- [x] **An always-open official game.** Done as one game per quarter
  (2026-10-30).
- [ ] **First-trade onboarding.** After `/welcome`, join the official game and
  land on Stocks, with a short checklist. Today it's about 7 steps.
- [x] **Hide game settings that do nothing.** The Rules page and the admin
  settings form show only the settings that work. Quarterly games have limit
  orders on.
- [x] **Enforce private games.** No longer needed: every quarterly game is
  public.
- [ ] **Achievements that never fire:** Champion, Podium Finish, Social Player,
  Market Watcher. Return badges are only checked after a market trade.
- [ ] **Stocks page:** show last price and day change, a sector filter and top
  movers.
- [ ] **Moderation:** hide chat messages, suspend users, link
  `/admin/messages` from the Admin page and email contact-form messages.
- [ ] **Promote courses and Questions** on the dashboard and in emails.
- [ ] **Live chat:** poll every 15 seconds and show relative times.
- [ ] **Benchmark against the S&P 500** rather than a flat line.
- [ ] **Shorter nav:** fold Achievements into another page. Templates and
  History are gone.
- [ ] **Real splits and dividends from Twelve Data**, so holdings don't break when
  a real split happens.
- [ ] **Small fixes:** the leaderboard rank skips hidden players; chat cuts
  messages at 500 bytes, not characters (can split an emoji).

## Follow-ups from the terms review (2026-10-29)

- [ ] **Check who runs StockGame.** Section 2 of the terms
  (`templates/pages/legal/terms.html`) names Local IT Help as the operator.
  Check with Rob whether it should name him instead.
- [ ] **Admin tool to remove chat messages.** Section 9 of the terms says we can
  remove chat messages, but there's no way to do it yet. Add a delete or hide
  action to `internal/handler/chat.go` for admins. Part of Moderation above.
- [ ] **Update the ASX wording.** The About page (`templates/pages/legal/about.html`)
  still says you can trade ASX 200 stocks. While the ASX licence is pending, the game offers
  US-listed Australian companies instead (`AusUS` in
  `internal/market/symbols.go`). Update the wording now, and again if the ASX
  licence comes through.
