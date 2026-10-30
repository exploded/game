# Email plan

Every email StockGame sends, why it sends it, and who can turn it off. All
email goes through Amazon SES.

## How sending works

- **Outbox.** Code never calls SES directly. It writes the message to the
  `email_outbox` table, and a background worker (`internal/mail`) sends it.
  A failed send retries with back-off, so an SES outage doesn't lose mail or
  slow down a request. Sign-in emails wake the worker straight away.
- **Sender.** `MAIL_FROM` (for example `StockGame <stockgame@mchugh.au>`).
  `mchugh.au` is already an SES identity. Marketing email from Rob sets
  `Reply-To` to Rob's address so replies reach him.
- **Dev.** Without SES credentials the worker prints each email to the log
  instead of sending it, so you can sign in locally by copying the link.
- **Format.** Every email has a plain-text part and an HTML part. HTML uses
  inline styles and no images, so it renders the same in Gmail and Outlook.
- **Rate.** The worker sends at most 10 a second, under the SES default of 14.

## Categories and preferences

Each email belongs to one category. People manage categories on the
**Settings** page, and every email except account email has a one-click
unsubscribe link for its category (plus the RFC 8058 `List-Unsubscribe`
header, which Gmail and Apple Mail show as an Unsubscribe button).

| Category | Key | Default | Can turn off |
|---|---|---|---|
| Account | `account` | On | No. Needed to sign in. |
| Trades | `trades` | On | Yes |
| Games | `games` | On | Yes |
| Weekly summary | `digest` | On | Yes. Hidden in Settings until D1 is built. |
| Community | `community` | On | Yes |
| Live streams | `live` | On | Yes |
| News from Rob | `news` | **Off** | Yes. Opt-in only. |

## The emails

**Built** means the code sends it today. **Planned** emails have their trigger
worked out but aren't written yet.

### Account

| # | Email | Trigger | Status |
|---|---|---|---|
| A1 | Sign-in link and code | Someone enters their email on `/login` | Built |
| A2 | Welcome | First sign-in creates the account | Built |
| A3 | Account deleted | User deletes their account on Settings | Built |
| A4 | Invite from a friend | A player enters a friend's email under **Invite a friend** on a game's home page | Built |

- **A1** has a link and a 6-digit code. The link suits someone reading on the
  same device. The code suits someone who reads email on their phone but plays
  on a laptop. Both expire after 15 minutes and work once.
- **A2** explains how a game works and links to the dashboard and help.
- **A4** goes to someone who isn't a user yet, so it has no unsubscribe link.
  It's sent once per address per game, only to addresses with no account,
  and each player can send 10 a day. It says who entered the address and that
  we haven't added them to a mailing list.

### Trades

| # | Email | Trigger | Status |
|---|---|---|---|
| T1 | Order filled | A limit order, or a market order queued after hours, fills | Built |
| T2 | Order cancelled or expired | An order can't fill (no cash, game over) or passes its expiry date | Built |
| T3 | Dividend paid | Admin runs a dividend on a stock you hold | Built |
| T4 | Stock split | Admin runs a split on a stock you hold | Built |

- These follow the in-app notifications. Each minute, `internal/notify` emails
  any new ones, one email per person and category, so five orders filling at
  the open make one email. Achievements stay in-app only.

### Games

| # | Email | Trigger | Status |
|---|---|---|---|
| G1 | Game started | A game you joined moves from pending to active | Built |
| G2 | Game ending soon | 3 days and 1 day before a game you're in ends | Built |
| G3 | Final results | A game you're in finishes: your rank, return and the top 3 | Built |
| G4 | Next round ready | A recurring game you created starts its next round | Removed: recurring games were replaced by one game per quarter |
| G5 | Referral bonus | A friend you invited signs up, and you get the game's referral bonus | Built |

- G2 and G3 matter most to Rob: they bring people back before a game ends.
- G1 to G3 go out after 8 am in `TIMEZONE`, once per game (the `game_emails`
  table records each). Games that started or finished before this shipped
  don't get them.

### Weekly summary

| # | Email | Trigger | Status |
|---|---|---|---|
| D1 | Your week | Monday 7 am in the server's `TIMEZONE` | Planned |

- Lists each active game: value, return, rank and rank change, best and worst
  holding, and days left. Adds new achievements and popular questions.
- Skipped for anyone not in an active game.

### Community

| # | Email | Trigger | Status |
|---|---|---|---|
| C1 | Your question was answered | An admin answers a question you asked or voted for | Built |
| C2 | New course | An admin publishes a course for the first time and ticks the email box | Built |

- C1 goes to the asker and everyone who voted, once: editing the answer
  doesn't resend it.
- C2 is optional on first publish only. It counts as Community email while
  courses are free. If Rob starts charging for courses, a new-course email
  promotes something for sale, so it must move to News (opt-in).

- C1 also goes when Rob answers a question during a live stream ("Answered
  on air" on the stream page). It waits until the stream ends and, for
  YouTube, until the recording is in, so the email links to the moment.

### Live streams

| # | Email | Trigger | Status |
|---|---|---|---|
| L1 | Rob is going live | A stream is first seen (YouTube sync or an admin adds one) at least an hour before it starts | Built |
| L2 | Rob is live now | A stream starts; only to people who tapped **Remind me** on it | Built |

- Category **Live streams**, on by default. Sent by `internal/media`
  (`live.go`); the scheduler runs it every minute.
- L2 also adds an in-app notification. It isn't sent for a stream found more
  than 30 minutes after it started.
- Like C2, this counts as service email while the streams are free. If a
  stream sells something (a paid course, Rob's advice service), its
  announcement is marketing and must go through News instead.

### News from Rob (marketing)

| # | Email | Trigger | Status |
|---|---|---|---|
| N1 | Newsletter or announcement | An admin writes and sends it from `/admin/email` | Built |
| N2 | We miss you | No sign-in for 30 days, at most once every 90 days | Planned |

- Only goes to people who ticked **Send me news from Rob** (unticked by
  default). The admin page shows the recipient count, sends a test to
  yourself first, then queues the rest. Sent newsletters are listed on the
  page (`email_broadcasts`).
- Won't send until `NEWS_SENDER` is set: the footer of every News email shows
  it.

## Legal: the Spam Act 2003

Marketing email (anything promoting Rob's courses, services or events) is a
commercial electronic message. To comply:

- **Consent.** Send only to people who opted in. The opt-in box is unticked by
  default, and we record when they ticked it (`email_prefs.updated_at`).
- **Identify the sender.** The footer shows `NEWS_SENDER`: who sends it and
  how to contact them. US recipients also need a postal address under
  CAN-SPAM, so include one. See the open question below.
- **Unsubscribe.** Every marketing email has a working unsubscribe link. It
  takes effect immediately (the Act allows 5 business days).

Game and trade emails are transactional, but they still carry an unsubscribe
link for their category.

## Setup before go-live

1. **IAM user for StockGame.** Done on 2026-10-30: the IAM user
   `stockgame-ses` has the policy `stockgame-ses-send`, which allows
   `ses:SendEmail` and `ses:SendRawEmail` only from `stockgame@mchugh.au`.
   If `MAIL_FROM` changes, update that policy too. Put the user's keys in the
   server's `.env` as `SES_ACCESS_KEY_ID` and `SES_SECRET_ACCESS_KEY`, and set
   `BASE_URL`, `SES_REGION=ap-southeast-2` and `MAIL_FROM`, plus `NEWS_SENDER`
   and `NEWS_REPLY_TO` once Rob confirms them.
2. **SES production access.** On, checked 2026-10-30: 50,000 emails a day,
   14 a second. `mchugh.au` is verified, with DKIM and the MAIL FROM domain
   `ses.mchugh.au`.
3. **Suppression list.** The account-level suppression list is on for
   bounces and complaints, so we never retry a dead address.
4. **Later, for marketing volume.** Send newsletters from a separate
   configuration set (or subdomain such as `news.mchugh.au`) so a complaint
   spike on a newsletter can't hurt sign-in email delivery.

## Open questions for Rob

- Which name and postal address go in the marketing footer? This ties to the
  terms follow-up about who runs StockGame.
- Which reply-to address should newsletters use?
- Is the weekly summary wanted at launch, or after the game emails?
