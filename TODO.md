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
- [x] **Rob's CPW courses.** The 7 video courses from consortiumpw.com.au/elearning
  (94 lessons) are copied in by the `import-cpw-courses` and `import-cpw-lessons`
  seed migrations (`courses_cpw.go`), published with no email. Courses have no
  thumbnail image.
- [ ] **From Rob:** are the 2018 super courses (Beginners Guide to Super, How
  Super Works) still right? How Super Works says employers pay 9.5% super;
  it's 12% now, and the videos may quote old caps and rules too. He can
  unpublish or edit them at `/admin/courses`.
- [x] **Review the whole game for improvements.** Findings are in the next
  section.
- [ ] **Go live.** Needs an SES IAM user and `SES_ACCESS_KEY_ID`,
  `SES_SECRET_ACCESS_KEY`, `SES_REGION` and `MAIL_FROM` in the server `.env`.
  Without them nobody can sign in. Remove `GOOGLE_CLIENT_*` from the `.env`.
- [x] **Newsletter details from Rob (2026-10-30)** are in the server `.env`:
  `NEWS_SENDER="Consortium Private Wealth, 24A Darlot Street, Horsham VIC 3400"`
  and `MAIL_REPLY_TO=info@consortiumpw.com.au` (was `NEWS_REPLY_TO`), and
  `MAIL_FROM=The Wealth Lab <hello@thewealthlab.au>`.
- [x] **Landing page team photo.** On the sign-in page: beside the form on a
  wide screen, under it on a phone (`static/img/team.jpg`, 2026-10-30).
- [ ] **Weekly summary and "we miss you" emails** (D1 and N2 in
  `docs/emails.md`). The Weekly summary setting is hidden until D1 exists.
- [ ] **Remove Google leftovers:** `users.picture_url`, the
  `*.googleusercontent.com` entry in the CSP `img-src`, and "Use Google Photo"
  on the Profile page.

## Learn: Rob's videos, live streams and questions (2026-10-30)

Built on 2026-10-30. Not deployed yet. `/learn` is public; asking, voting and
reminders need an account.

- [x] **Watch.** Rob's YouTube channel syncs every 10 minutes (whole channel
  nightly). Claude sorts each video into topics (retirement, super, buying a
  home and so on); admins can retick them. Rows by topic, Shorts, search,
  click-to-play players, related videos, "Ask Rob about this".
- [x] **Live.** YouTube streams appear by themselves; admins add TikTok,
  Facebook and Instagram streams or tap "I'm live now". Site-wide "Rob is
  live" banner, Remind me, Add to calendar, and Live streams email (L1, L2 in
  `docs/emails.md`).
- [x] **Live Q&A queue.** The stream page lists the most-voted open questions
  and refreshes every 20 seconds. Rob taps **Answered** as he goes; voters get
  an email with a link to that moment once the recording is in.
- [x] **Questions moved into Learn** (`/questions` redirects), with topics,
  and answers can be a video from a given time.
- [x] **TikTok through TikTok's API.** Rob connects once from `/admin/learn`;
  the access token refreshes itself and lasts a year. Covers are cached
  because TikTok's links expire after six hours.
- [x] **Stock pages show Rob's videos** about that stock, and the dashboard
  shows new videos and the next stream.
- [ ] **Go live:** add `YOUTUBE_API_KEY` and `ANTHROPIC_API_KEY` to the server
  `.env`. Sorting the first 1,600 videos takes about 90 minutes and costs
  roughly US$5 to US$10.
- [ ] **TikTok app:** register at developers.tiktok.com (Login Kit, Display
  API), pass TikTok's app review, set `TIKTOK_CLIENT_KEY` and
  `TIKTOK_CLIENT_SECRET`, then Rob clicks **Connect TikTok**. Reconnect
  before the year is up; the admin page warns 30 days ahead.
- [ ] **From Rob:** whether the topic list suits him (`/admin/learn/topics`).
  His general advice wording is in; no podcast link is needed (2026-10-30).
- [ ] **Later:** Facebook and Instagram sync and Facebook live detection
  through Meta's Graph API (needs Rob's Page connected to a Meta app). TikTok
  LIVE has no API, so TikTok streams stay manual. Web push notifications.
- [ ] **Check on a phone:** `/learn`, a video, `/learn/live`, and a stream
  page while live.

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
- [x] **Monthly instead of quarterly** (2026-10-30). The Oct–Dec 2026 game
  ended on 31 October as "October 2026"; Jan–Mar 2027 was removed.
- [ ] **Email everyone when a month's game opens**, not only players who
  joined early. Today G1 (game started) goes only to players already in it.

## From the whole-game review (2026-10-29)

Highest impact for the effort first.

- [x] **Recurring games made a new round every minute** once a round finished.
  Fixed, with a test. No recurring game had finished in production, so no one
  was affected.
- [x] **Invite links.** Replaced by inviting a friend by email from the
  game's home page; the bonus is paid when the friend signs up (2026-10-30).
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
- [ ] **Promote courses and Questions** on the dashboard and in emails. The
  dashboard now shows Rob's new videos and next stream (Learn).
- [ ] **Live chat:** poll every 15 seconds and show relative times.
- [ ] **Benchmark against the S&P 500** rather than a flat line.
- [ ] **Shorter nav:** fold Achievements into another page. Templates and
  History are gone.
- [ ] **Real splits and dividends from Twelve Data**, so holdings don't break when
  a real split happens.
- [ ] **Small fixes:** the leaderboard rank skips hidden players; chat cuts
  messages at 500 bytes, not characters (can split an emoji).

## Follow-ups from the terms review (2026-10-29)

- [x] **Check who runs StockGame.** Rob says Consortium Private Wealth; the
  terms name it (2026-10-30).
- [ ] **Admin tool to remove chat messages.** Section 9 of the terms says we can
  remove chat messages, but there's no way to do it yet. Add a delete or hide
  action to `internal/handler/chat.go` for admins. Part of Moderation above.
- [ ] **Update the ASX wording.** The About page (`templates/pages/legal/about.html`)
  still says you can trade ASX 200 stocks. While the ASX licence is pending, the game offers
  US-listed Australian companies instead (`AusUS` in
  `internal/market/symbols.go`). Update the wording now, and again if the ASX
  licence comes through.
