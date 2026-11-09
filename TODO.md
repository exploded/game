# To do

Ideas and requests, with what's done ticked. `FEATURES.md` describes each
feature once it's built.

## Requests from Rob (2026-10-29)

Built on 2026-10-29, merged into `master` and deployed on 2026-10-30.

- [x] **Sign up with email, no Google.** Google sign-in is removed. `/login`
  emails a link plus a 6-digit code, valid for 15 minutes. New players pick a
  display name and opt in to news on `/welcome`. Existing users keep their
  account and sign in with the same email.
- [x] **Email through Amazon SES.** Outbox table with retries, per-category
  settings on Settings, one-click unsubscribe. Every email is planned in
  `docs/emails.md`.
- [x] **Trade and game emails:** orders filled, cancelled or expired,
  splits, game started, ending in 3 days and 1 day, final results,
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
- [x] **Rob's 2026 courses** (2026-11-09). The ten courses from Anne and Rob's
  sheet are seeded as drafts (`courses_2026.go`), with covers and "What you'll
  learn" lists. Courses moved to `/learn/courses` and went public (lessons
  still need an account), and Learn got a front page with graphics.
- [x] **Vimeo token and plan check** (2026-11-09). Consortium's Vimeo is on
  the paid **Plus** plan, so the courses stay on Vimeo (no Bunny). James's
  token is in his user env (`VIMEO_TOKEN`). All 11 course folders' videos are
  unlisted with embedding allowed, so they play on the site as they are.
- [ ] **Optional: lock the course videos to our domain.** On Vimeo, set each
  course folder's videos to embed only on investorproject.au (and
  consortiumpw.com.au if they're used there too). Do it after a lesson has
  played on prod; localhost won't play them afterwards.
- [ ] **Fill and publish the eleven courses** on prod: put `VIMEO_TOKEN` in the
  server `.env`, use Fill from Vimeo on each, play a lesson of each on a
  phone, publish (no email, or one), then unpublish the 7 old CPW courses
  (the 2018 super ones quote 9.5% super; it's 12% now).
- [ ] **From Anne:** which "Building Your First Home Deposit" Vimeo folder is
  current (December 2025, 7 videos, or February 2026, 6)?
- [x] **Rob's investment presentation** (2026-11-09): James confirmed it
  belongs. Seeded as the draft "How to Invest for the Long Term" (folder
  26617132, 10 parts, 67 min), with a summary written from
  consortiumpw.com.au/investment-presentation. Rob to check the wording.
- [x] **Review the whole game for improvements.** Findings are in the next
  section.
- [x] **Go live.** SES keys and `MAIL_FROM` are in the server `.env`, and
  sign-in email arrives from hello@thewealthlab.au (2026-10-30).
- [x] **Newsletter details from Rob (2026-10-30)** are in the server `.env`:
  `NEWS_SENDER="Consortium Private Wealth, 24A Darlot Street, Horsham VIC 3400"`
  and `MAIL_REPLY_TO=info@consortiumpw.com.au` (was `NEWS_REPLY_TO`), and
  `MAIL_FROM=The Investor Project <hello@thewealthlab.au>`.
- [x] **Landing page team photo.** On the sign-in page: beside the form on a
  wide screen, under it on a phone (`static/img/team.jpg`, 2026-10-30).
- [ ] **Weekly summary and "we miss you" emails** (D1 and N2 in
  `docs/emails.md`). The Weekly summary setting is hidden until D1 exists.
- [x] **Remove Google leftovers:** `users.picture_url`, the
  `*.googleusercontent.com` entry in the CSP `img-src`, and "Use Google Photo"
  on the Profile page. Done on 31 October 2026, with the rest of the cleanup
  in `docs/site-summary.md`.

## Learn: Rob's videos, live streams and questions (2026-10-30)

Built and deployed on 2026-10-30. `/learn` is public; asking, voting and
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
- [x] **TikTok streams start and end by themselves** (2026-10-31). From 15
  minutes before a booked TikTok stream until 2 hours after, the site asks
  TikTok's own live endpoint every minute whether Rob is live (no login or
  key; not a public API, so it may break). It ends a stream it saw start once
  he's been off air for 5 minutes. The Add a stream form fills itself in from
  the last stream on the platform chosen.
- [x] **All-day TikTok check and log** (2026-10-31). Rob's account is also
  checked every 5 minutes from 6 am to 10 pm; a stream he didn't book is added
  (banner only, no email). A 403 or 429 from TikTok stops the all-day checks
  until the next day. Every check is logged in `tiktok_live_checks` (30 days)
  and listed on `/admin/learn/live`.
- [ ] **Watch the log for a week:** if TikTok refuses checks from the
  Linode, cut the all-day check back or drop it.
- [x] **TikTok's live status checked** (2026-10-31) on accounts streaming
  at the time: `liveRoom.status` is 2 while live, with `startTime` and
  `title`. TikTok blocked a home IP (403) after about 30 requests in 5
  minutes, so keep polling light.
- [x] **Watch Rob's first booked TikTok stream** start and end by itself
  (2026-11-05).
- [x] **Live page tidied** (2026-11-05). TikTok keeps no recording of a live
  stream for viewers, so the "Watch on TikTok" link on a past TikTok stream
  led to an empty page. Past streams now list only those with a recording
  (as video cards) and those Rob answered questions in. A stream page says
  when there's no recording and shows its length. A TikTok stream starts at
  TikTok's own start time and ends at the last check that found Rob on air
  (it used to end 5 minutes late). Admins can attach a recording to a
  finished stream.
- [ ] **For Rob: save your TikTok live streams** (2026-11-05). TikTok doesn't
  keep a live stream for viewers, so the site can't show one afterwards.
  After each stream, Rob saves the replay (TikTok lets the host download it
  for 90 days) and posts it to TikTok or YouTube as an ordinary video. Once
  the TikTok app is approved and connected, the sync adds it to the site by
  itself (YouTube already does). Then, on the stream's page on the site, an
  admin pastes the video's link under **Admin: run this stream → Recording**,
  which puts the stream back on the Live page with a player. Rob also has to
  link each question he answered in the stream to that video by hand, on the
  question's page (Answer with a video, from the time he answers it), because
  the times recorded during the stream may not match the saved video.
- [x] **Stock pages show Rob's videos** about that stock, and the dashboard
  shows new videos and the next stream.
- [x] **Go live:** `YOUTUBE_API_KEY` and `ANTHROPIC_API_KEY` are in the server
  `.env` (2026-10-30).
- [ ] **TikTok app:** register at developers.tiktok.com (Login Kit, Display
  API), pass TikTok's app review, set `TIKTOK_CLIENT_KEY` and
  `TIKTOK_CLIENT_SECRET`, then Rob clicks **Connect TikTok**. Reconnect
  before the year is up; the admin page warns 30 days ahead.
- [ ] **Pasted TikToks have no cover** (2026-11-05). TikTok's oEmbed no
  longer gives a title or cover, so a TikTok pasted at `/admin/learn` needs a
  typed title and shows a plain cover. Once the TikTok app is approved and
  connected, the sync adds the missing covers by itself (videos 63427 and
  63430 on prod). Check they appear then; if the app is refused, add a cover
  upload to the video's admin page.
- [ ] **From Rob:** whether the topic list suits him (`/admin/learn/topics`).
  His general advice wording is in; no podcast link is needed (2026-10-30).
- [ ] **Later:** Facebook and Instagram sync and Facebook live detection
  through Meta's Graph API (needs Rob's Page connected to a Meta app). TikTok
  streams still have to be booked by hand. Web push notifications.
- [ ] **Check on a phone:** `/learn`, a video, `/learn/live`, and a stream
  page while live.

## One game a month (2026-10-30)

Built and deployed on 2026-10-30, first as quarterly games.

- [x] **One game at a time, one each month.** Starts at midnight on the 1st and
  ends at midnight after the month's last day. The scheduler creates this
  month's game and the next one, copying settings forward. Players can't
  create games; templates and recurring games are gone.
- [x] **Past games** are listed on `/games` with their final standings.
- [x] **Only admins change game settings**, at `/admin/games`.
- [x] **Cancel "Test 3" in production.** Gone with private games (ccfa785).
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
- [x] **Achievements that never fire:** Champion, Podium Finish, Social Player
  and Market Watcher are now awarded (2026-10-31, `market.GrantAchievement`).
- [x] **Stocks page:** last price, today's move, a sector menu, and Gainers
  and Losers chips (2026-10-30).
- [ ] **Moderation:** done except emailing contact-form messages to admins.
  Admins delete lobby messages from `/lobby`, block users at `/admin/users`,
  and Messages is in the admin links (2026-10-31).
- [ ] **Promote courses and Questions** on the dashboard and in emails. The
  dashboard now shows Rob's new videos and next stream (Learn).
- [ ] **Live lobby:** poll every 15 seconds and show relative times.
- [ ] **Benchmark against the S&P 500** rather than a flat line.
- [ ] **Shorter nav:** fold Achievements into another page. Templates and
  History are gone.
- [x] **Real splits from Twelve Data**, so holdings don't break when a real
  split happens (`market.CheckSplits`).
- [ ] **Small fixes:** the leaderboard rank skips hidden players; the lobby cuts
  messages at 500 bytes, not characters (can split an emoji).

## Follow-ups from the terms review (2026-10-29)

- [x] **Check who runs StockGame.** Rob says Consortium Private Wealth; the
  terms name it (2026-10-30).
- [x] **Admin tool to remove chat messages.** Admins see Delete on each lobby
  message (`AdminLobbyDelete` in `lobby.go`, 2026-10-31).
- [x] **Update the ASX wording.** No page mentions ASX now. Update the wording
  again if the ASX licence comes through.

## Capacity (2026-11-06)

How many players the site can take, measured with `cmd/loadtest`. The
numbers, limits and how to measure again are in `docs/capacity.md`.

- [x] **Load test and report.** Measured on the PC and on a temporary
  Linode 4 GB the same as production. The limit is the one SQLite
  connection, not CPU or memory.
- [x] **Limits** (860b880, b4d6904). The app works on 8 requests at once and
  answers the rest with a quick "busy" page, and the `game` service has
  memory and CPU caps so it can't starve the other apps. The server's deploy
  script had never updated itself from the repo; fixed.
- [x] **Learn pages 4–8 times faster, the stocks list rebuilt in the
  background, sign-in email sent first** (fa7f4fe).
- [x] **Leaderboard from counts, 100 a page** (9e42f6b). At 10,000 players
  the site went from under 10 requests a second to about 120.
- [ ] **"Players' favourites" chip.** It counts every holding in the game on
  each view: about 70 ms at 10,000 players, the only page cost that still
  grows with players. Cache the counts for a minute, as the stocks list is.
  Small.
- [ ] **Stock page chart.** Each view reads the stock's whole price history
  (10 ms), and the candles read it again. It's the most-viewed page, so cache
  each stock's chart data until the next price update. Small to medium.
- [ ] **Revaluation.** 7.7 seconds every 2 minutes in the US session at
  10,000 players (about 6% of the database's time, overnight in Sydney). Look
  up each stock's price once, write in batches and skip unchanged holdings.
  Before about 30,000 players, or sooner if overseas or AI players grow
  during US hours. Medium; it handles money, so it needs care.
- [ ] **Queries that read whole tables** (`ExpireOldLimitOrders`,
  `DeleteOldEmails`, `CountLobbyMessages`): fine now, revisit past about
  100,000 rows.
- [ ] **Snapshot dates** are UTC, but the month chart picks them by the
  game's Sydney dates, so a revaluation in the first 11 hours of the 1st is
  left off the chart. Cosmetic.
- [ ] **Watch for "server busy" warnings in monitor:** they're the sign that
  load is starting to bite. Re-run the load test after changing anything
  every page uses.

## Promotion

- [ ] **Get listed on Finder** (2026-11-05). Ask Finder to add the share game
  to its share trading games page:
  https://www.finder.com.au/share-trading/share-trading-games. Check with Rob
  first, since the listing names Consortium Private Wealth's site.

## Ideas for Rob to approve

- [ ] **Drop Facebook and Instagram videos from Learn?** (2026-11-05). Today
  an admin can paste a Facebook or Instagram video link at `/admin/learn`, and
  add Facebook and Instagram live streams by hand. Neither platform syncs
  (that needs Rob's Page connected to a Meta app), neither gives a title or
  cover, and Rob's videos are mostly on YouTube and TikTok anyway. Removing
  them would simplify the paste form, the platform filter and the live stream
  form, and take `www.facebook.com` and `www.instagram.com` out of the CSP's
  `frame-src`. For Rob to decide whether he posts anything only there.

- [ ] **Learn: public or behind sign-in?** (2026-10-31). Today anyone can
  read `/learn`: videos, topics, live streams and Ask Rob questions with their
  answers. Asking, voting and reminders need an account. The sitemap lists the
  topics, the latest videos and every answered question, so Google can find
  them. Courses are public too since 2026-11-09 (outlines and lesson titles;
  playing a lesson needs an account). For Rob to decide:
  - **Keep it public (as now).** Search engines can index it, so people
    searching for super, retirement or home-buying questions can land on the
    site. Answered questions help most, because they're Rob's own words; the
    videos are on YouTube already.
  - **Put it behind sign-in.** Everyone who watches has an account and can be
    emailed, but Google sees only the sign-in page, and a shared video link
    asks for sign-in first.

- [ ] **Share your result on social media** (2026-10-31). A Share button on
  the game's home page: the phone's own share sheet on a phone, and Copy link,
  X, Facebook, LinkedIn and WhatsApp on a computer. Game pages need a sign-in,
  so the link goes to a public page, `/share/{token}` (random per player per
  game, and the player can turn it off). The page shows the player's first
  name, the month, their rank and return since the start in percent, and a
  **Play The Investor Project** button to sign up. A 1200 × 630 preview image drawn
  in Go makes the post show as a card. About a day's work. For Rob to decide:
  - **No dollar amounts**, so a post doesn't read like "$1,120,000" of real
    money. Optionally the symbols of the three biggest holdings, no amounts.
  - **First name only**, or the full name.
  - **Compliance:** CPW is a licensed adviser, and posts showing returns from
    a site it runs may count as its marketing. The page and image would say
    "Play money. Not financial advice." CPW's compliance should check first.
  - **No referral bonus** for sign-ups from a share link, so it doesn't get
    around the one invite per game.
