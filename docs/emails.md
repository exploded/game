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
| Weekly summary | `digest` | On | Yes |
| Community | `community` | On | Yes |
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

- **A1** has a link and a 6-digit code. The link suits someone reading on the
  same device. The code suits someone who reads email on their phone but plays
  on a laptop. Both expire after 15 minutes and work once.
- **A2** explains how a game works and links to the dashboard and help.

### Trades

| # | Email | Trigger | Status |
|---|---|---|---|
| T1 | Order filled | A limit order, or a market order queued after hours, fills | Planned |
| T2 | Order expired | A limit order reaches its expiry date unfilled | Planned |
| T3 | Dividend paid | Admin runs a dividend on a stock you hold | Planned |
| T4 | Stock split | Admin runs a split on a stock you hold | Planned |

- When several orders fill in one scheduler run, send one email listing them
  all, not one email each.

### Games

| # | Email | Trigger | Status |
|---|---|---|---|
| G1 | Game started | A game you joined moves from pending to active | Planned |
| G2 | Game ending soon | 3 days and 1 day before a game you're in ends | Planned |
| G3 | Final results | A game you're in finishes: your rank, return and the top 3 | Planned |
| G4 | Next round ready | A recurring game you were in starts its next round | Planned |
| G5 | Friend joined | Someone joins a game through your invite link | Planned |

- G2 and G3 matter most to Rob: they bring people back before a game ends.
- At most one game email per game per day, apart from G3.

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
| C1 | Your question was answered | Rob (or an admin) answers your question | Planned |
| C2 | New course | A course is published | Planned |

- These depend on the Ask a Question and Courses features.

### News from Rob (marketing)

| # | Email | Trigger | Status |
|---|---|---|---|
| N1 | Newsletter or announcement | An admin writes and sends it from `/admin/email` | Planned |
| N2 | We miss you | No sign-in for 30 days, at most once every 90 days | Planned |

- Only goes to people who ticked **Send me news from Rob** (unticked by
  default). The admin page shows the recipient count, sends a test to
  yourself first, then queues the rest.

## Legal: the Spam Act 2003

Marketing email (anything promoting Rob's courses, services or events) is a
commercial electronic message. To comply:

- **Consent.** Send only to people who opted in. The opt-in box is unticked by
  default, and we record when they ticked it (`email_prefs.updated_at`).
- **Identify the sender.** The footer names who runs StockGame and how to
  contact them. US recipients also need a postal address under CAN-SPAM.
  See the open question below.
- **Unsubscribe.** Every marketing email has a working unsubscribe link. It
  takes effect immediately (the Act allows 5 business days).

Game and trade emails are transactional, but they still carry an unsubscribe
link for their category.

## Setup before go-live

1. **IAM user for StockGame.** Create an IAM user with only
   `ses:SendRawEmail` on the `mchugh.au` identity, and put its keys in the
   server's `.env` as `SES_ACCESS_KEY_ID` and `SES_SECRET_ACCESS_KEY`. Set
   `SES_REGION=ap-southeast-2` and `MAIL_FROM`.
2. **Check SES production access.** A sandboxed SES account can only send to
   verified addresses. clearsky already mails the public, so this is probably
   done, but check the SES console.
3. **Suppression list.** Turn on the SES account-level suppression list for
   bounces and complaints, so we never retry a dead address.
4. **Later, for marketing volume.** Send newsletters from a separate
   configuration set (or subdomain such as `news.mchugh.au`) so a complaint
   spike on a newsletter can't hurt sign-in email delivery.

## Open questions for Rob

- Which name and postal address go in the marketing footer? This ties to the
  terms follow-up about who runs StockGame.
- Which reply-to address should newsletters use?
- Is the weekly summary wanted at launch, or after the game emails?
