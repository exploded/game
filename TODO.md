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
- [ ] **From Rob:** are the 2018 super courses (Beginners Guide to Super, How
  Super Works) still right? How Super Works says employers pay 9.5% super;
  it's 12% now, and the videos may quote old caps and rules too. He can
  unpublish or edit them at `/admin/courses`.
- [x] **Review the whole game for improvements.** Findings are in the next
  section.
- [x] **Go live.** SES keys and `MAIL_FROM` are in the server `.env`, and
  sign-in email arrives from hello@thewealthlab.au (2026-10-30).
- [x] **Newsletter details from Rob (2026-10-30)** are in the server `.env`:
  `NEWS_SENDER="Consortium Private Wealth, 24A Darlot Street, Horsham VIC 3400"`
  and `MAIL_REPLY_TO=info@consortiumpw.com.au` (was `NEWS_REPLY_TO`), and
  `MAIL_FROM=The Wealth Lab <hello@thewealthlab.au>`.
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
- [ ] **Check the TikTok live check during a real stream:** that TikTok gives
  `liveRoom.status` 2 while Rob is live, and the stream starts by itself.
- [x] **Stock pages show Rob's videos** about that stock, and the dashboard
  shows new videos and the next stream.
- [x] **Go live:** `YOUTUBE_API_KEY` and `ANTHROPIC_API_KEY` are in the server
  `.env` (2026-10-30).
- [ ] **TikTok app:** register at developers.tiktok.com (Login Kit, Display
  API), pass TikTok's app review, set `TIKTOK_CLIENT_KEY` and
  `TIKTOK_CLIENT_SECRET`, then Rob clicks **Connect TikTok**. Reconnect
  before the year is up; the admin page warns 30 days ahead.
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

## Ideas for Rob to approve

- [ ] **Learn: public or behind sign-in?** (2026-10-31). Today anyone can
  read `/learn`: videos, topics, live streams and Ask Rob questions with their
  answers. Asking, voting and reminders need an account. The sitemap lists the
  topics, the latest videos and every answered question, so Google can find
  them. Courses (`/courses`, the 94 CPW lessons) need sign-in. For Rob to
  decide:
  - **Keep it public (as now).** Search engines can index it, so people
    searching for super, retirement or home-buying questions can land on the
    site. Answered questions help most, because they're Rob's own words; the
    videos are on YouTube already.
  - **Put it behind sign-in.** Everyone who watches has an account and can be
    emailed, but Google sees only the sign-in page, and a shared video link
    asks for sign-in first.
  - **Courses public too?** Only if CPW is happy for its eLearning to be
    indexed here as well as on consortiumpw.com.au.

- [ ] **Share your result on social media** (2026-10-31). A Share button on
  the game's home page: the phone's own share sheet on a phone, and Copy link,
  X, Facebook, LinkedIn and WhatsApp on a computer. Game pages need a sign-in,
  so the link goes to a public page, `/share/{token}` (random per player per
  game, and the player can turn it off). The page shows the player's first
  name, the month, their rank and return since the start in percent, and a
  **Play The Wealth Lab** button to sign up. A 1200 × 630 preview image drawn
  in Go makes the post show as a card. About a day's work. For Rob to decide:
  - **No dollar amounts**, so a post doesn't read like "$1,120,000" of real
    money. Optionally the symbols of the three biggest holdings, no amounts.
  - **First name only**, or the full name.
  - **Compliance:** CPW is a licensed adviser, and posts showing returns from
    a site it runs may count as its marketing. The page and image would say
    "Play money. Not financial advice." CPW's compliance should check first.
  - **No referral bonus** for sign-ups from a share link, so it doesn't get
    around the one invite per game.
