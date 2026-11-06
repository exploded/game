# The Wealth Lab: what the site does

A plain-language guide to every part of thewealthlab.au, as at 31 October
2026, and what was tidied up that day.

The Wealth Lab is Rob Goudie's investing community, run by Consortium Private
Wealth. It has two halves:

- **The share game**: a free monthly competition where players invest pretend
  money in real shares.
- **Learn**: Rob's videos, live streams, questions and courses.

## Signing in

- There are no passwords. You type your email address, and the site emails you
  a link and a 6-digit code. Either one signs you in, once, within 15 minutes.
- If the address is new, the site makes an account. A new player picks a
  display name and chooses whether to get news from Rob.
- You stay signed in for 30 days on that device.
- Someone invited by a friend gets a link that takes them straight to sign-up.

## The share game

### How a game works

- There is one game each calendar month, named after the month (for example,
  "November 2026"). It runs from midnight on the 1st to midnight after the last
  day of the month, Melbourne time.
- Everyone gets $1,000,000 (Australian dollars) of pretend money to start.
- Players are put in this month's game automatically when they sign in. Nobody
  has to find or join it.
- Next month's game is open early, and players can join it with one button.
- The player with the most valuable portfolio when the month ends wins.
- Admins can change a game's settings (see [Admin tools](#admin-tools)), and
  each new month copies the settings of the month before.

### What players can buy

- Shares in the S&P 500 (the 500 biggest US companies).
- Australian companies listed in the US, and big overseas companies listed in
  the US, such as ASML, TSMC, Novo Nordisk and Shopify.
- Two US Bitcoin shares outside the S&P 500: the iShares Bitcoin Trust ETF
  (IBIT), which follows the Bitcoin price, and Strategy (MSTR), which holds
  Bitcoin. Crypto itself isn't offered.
- SpaceX (SPCX), which listed on the Nasdaq in June 2026 and isn't in the
  S&P 500 yet.
- Australian (ASX) shares aren't offered until a data licence is in place.
- Prices are real, from a data company called Twelve Data. While the US market
  is open, prices update every 2 minutes.
- US shares are priced in US dollars. The site converts them at the real
  Australian dollar exchange rate, updated every 15 minutes.

### Getting around

Every signed-in page has the same five sections: **Home**, **Stocks**,
**Ranks**, **Learn** and **Chat** (the lobby). On a phone they're a row of icons
at the bottom of the screen; on a computer they're links in the bar at the top.
The bar at the top also has the logo (back to Home), the player's cash on a
game's pages (tap it to open the game's home page), a bell for notifications with the unread count, and the player's
picture, which opens a menu: Orders, My history, Achievements, Game rules, Past
and upcoming games, Settings, Connect your AI, Help and Sign out. (Before 3
November 2026 the site had two bars of links and a separate game bar, and the
phone's icons showed only on game pages.)

The sections always open this month's game. On a page of another game, such as
last month's, a strip under the top bar names that game and links back to this
month's.

### Home

Signing in, the logo and **Home** open the home page (since 4 November 2026,
laid out and worded from Rob's design). Under the heading "Build real skills.
Play for a brighter future." are three big tiles:

- **Game Home**: straight into this month's game (players are always in it),
  showing what their portfolio is worth and their place.
- **Ask a Question**: send Rob a question for the next TikTok Q&A Live, with
  the next live's date when one is booked.
- **Learn**: Rob's YouTube videos, TikTok Live replays and courses.

Under the tiles is Rob's intro video on what the site is about, then three
lines (Compete, Learn, Be part of it) and "Practise today. Invest in
tomorrow." The same video is on the sign-in page for people who haven't
joined yet.

#### Intro video

The video is a file on our own site, not YouTube, so it plays in a plain
player with no YouTube branding. It's built into the program with the CSS and
images, so it ships with every deploy. For now it's a placeholder: a short tour
of the site with "Inspired" by Kevin MacLeod as music. That track is licensed
CC BY 4.0, so its credit must stay while the track is used. It's under
Credits on the About page, not under the video. The originals are in `.local/video` (not in git).

To replace it with Rob's video:

1. Make a web copy at 720p, with the index at the front so it starts playing
   straight away (in PowerShell, in the folder with Rob's file):
   `ffmpeg -i rob.mp4 -vf scale=-2:720 -c:v libx264 -preset slow -crf 23 -pix_fmt yuv420p -c:a aac -b:a 128k -movflags +faststart the-wealth-lab-tour.mp4`
2. Make a cover from a frame, here 1 second in:
   `ffmpeg -ss 1 -i rob.mp4 -frames:v 1 -vf scale=1280:-2 -q:v 4 the-wealth-lab-tour.jpg`
3. Replace both files in `static/video`, change or remove the music credit on
   the About page (`templates/pages/legal/about.html`), then build and deploy.

Keep it under about 20 MB; every deploy carries it.

### The game's home page

When a player opens the game (the Game Home tile) they see:

- The game's name and how many days are left.
- What their portfolio is worth, how much it moved today and since the game
  started, their place on the leaderboard and how far they are behind the
  player above, and a chart of their portfolio's value across the month, all in
  one card.
- **Waiting orders**, when they have orders that haven't filled yet.
- Each share they hold, with today's move and the overall gain or loss, and a
  **Close position** button to sell it all in one tap.
- **Sectors**: how their shares are spread across industries, one bar each.
- **Invite a friend** (see [Inviting friends](#inviting-friends)).

A player who hasn't bought anything yet sees three steps explaining the game,
then the first five of Rob's Picks to tap straight into, and a search box.

When a game is over, the page shows the final values from the end of the game.

### Finding shares

The **Stocks** page lists shares with each company's logo, latest price, today's
move and a small 30-day chart. Quick filters pick a list:

- **Watchlist** (shares the player has starred; it carries over from one
  month's game to the next)
- **Rob's Picks** (the default, chosen by Rob)
- **Mag 7** (Apple, Microsoft, Amazon, Nvidia, Alphabet, Meta and Tesla)
- **Tech**, **Gainers**, **Losers**, **Biggest** and **Most traded**
- **Aussie** and **International** (companies from Australia and overseas)
- **Crypto** (Bitcoin funds and crypto companies such as Coinbase)
- **Players' favourites** (most held by players in this game)
- **All A–Z**

Players can also search every share by name or code, and filter by industry.

After those lists, more buttons pick shares by the company's latest reported
figures: making a profit (biggest profit first) or a loss (biggest loss first),
positive free cash flow, profit margin over 20%, P/E under 15, PEG under 1,
revenue up 15% or more, earnings up on last year, debt-to-equity under 0.5, more
cash than debt, or a health score of 4 or 5. Players pick one button at a time;
the industry filter still narrows any of them. Each share shows the figure it
matched on, and a line under the heading explains the measure. The labels say
what the test is, not whether a share is a good buy.

Every share with suitable figures carries a **health score** out of 5: a point
each for a profit, positive free cash flow, debt-to-equity under 1, a current
ratio over 1 and revenue up on a year earlier. Banks and insurers don't get one,
because those measures don't suit them. The figures come from Twelve Data and
are updated weekly.

Each share has its own page with a long-term price chart (a simple line on a
phone, which shows the date and price under a finger dragged across it, and
candles on a computer), then the player's own
position in it, eight key figures (market value, P/E, 52-week high and low,
average volume, beta, industry and health score), a short company description
that opens in full when tapped, the rest of the company's figures folded into
five groups, recent news, and any of Rob's videos that mention it.

### Buying and selling

- Players trade from the order panel on a share's page. On a phone it slides up
  from the bottom when they tap **Buy** or **Sell**.
- **Market orders** buy or sell at the latest price.
- **Limit orders** wait until the price reaches a level the player chooses, then
  fill by themselves.
- When the US market is closed (most of the Australian day), a market order
  fills straight away at the last closing price. Admins can switch this off for
  a game, so that orders wait for the market to open instead.
- **Short selling** (betting a share will fall) is available only if an admin
  turns it on for that game. It's off by default.
- Open orders show on the home page and on the **Orders** page (in the menu
  under the player's picture), where they can be cancelled. Orders still open
  when a game ends are cancelled.

### Other game pages

- **Ranks** has two halves: **Players**, every player who has made a trade,
  ranked by portfolio value (a player with no trade is told they'll appear
  after their first, and their home page says "Not ranked yet"), and **Activity**, everything happening in the game, newest
  first: every player's trades (with the company's name, linked to its page),
  who joined, and referral bonuses earned.
- **My history**: the player's own trades, interest, dividends and referral bonuses, with
  the cash balance after each. A summary at the top adds up from the opening
  balance through the cash to the total on the leaderboard (in the menu under
  the player's picture, and linked from Game activity and the home page).
- **Rules**: the game's settings and dates.
- **Past games**: finished games and their final standings, linked from the
  bottom of the home page.

### Extras that happen by themselves

- **Interest on cash**: uninvested cash earns interest each day, at 5.25% a
  year unless an admin changes it.
- **Share splits**: when a company splits its shares (for example, 10 for 1),
  players' holdings and open orders are adjusted automatically, and holders are
  told.
- **Dividends** (from 2 November 2026): when a share goes ex-dividend, everyone
  holding it at that day's market open gets the dividend as cash, and is told.
  A player who is short the share pays the dividend instead, as in real life.
  Without this, holding a dividend payer lost money each time its price dropped
  by the dividend.

### Inviting friends

From the game's home page a player can email one friend an invitation per
game. When the friend makes an account, they join the game and the player
gets a bonus of 1% of the starting money ($10,000) in game money, in that
game, or in the current month's game if that one has ended. People who already
have an account can't be invited.

### Achievements and avatars

Players earn badges for things they do:

| Badge | How to earn it |
|---|---|
| First Trade | Make a first trade |
| Active Trader | 10 trades, across all games |
| Trading Machine | 50 trades, across all games |
| Rising Star | A 10% return |
| Market Wizard | A 25% return |
| Diversified | Hold 5 different shares |
| Portfolio Manager | Hold 10 different shares |
| Champion | Finish a game in 1st place |
| Podium Finish | Finish a game in the top 3 |
| Social Player | Play in 3 games |
| Market Watcher | Put 10 shares on your watchlist |
| Recruiter | Invite 3 friends who sign up |

Champion and Podium Finish count only players who made at least one trade, so
someone who joined and never traded can't win on interest alone. Players who
finish level share the place. Each badge is earned once, not once per game.

Under **Change avatar** in Settings players choose an avatar picture. Nine are free; four
(Crown, Wizard, On Fire and Trophy) unlock with a badge.

### Talking to other players

The **Lobby** is a site-wide chat room for all signed-in players. Claude
checks each message against the chat rules in the terms before it's posted. A
blocked message isn't posted: the sender sees why, keeps the text to change
it, and can tell the moderators through Contact if it was a mistake. Blocked
messages stay in the lobby for admins only, marked Blocked with the reason, so
they can check them (see Admin tools). Admins can also delete any message.

## Learn

Learn is open to everyone, signed in or not. It has four tabs, which also show
on the course pages. The Learn section opens Ask Rob.

- **Ask Rob**: members post questions and vote for the ones they want answered.
  Rob answers in writing, with a video, or live on air, and the asker and
  everyone who voted get the answer by email. During a live stream, the stream
  page shows the most-voted questions.
- **Watch**: Rob's YouTube videos (and TikTok, once TikTok approves the
  connection), sorted into topics such as retirement, superannuation and buying
  a home. Visitors can search, browse by topic, and watch without leaving the
  site. A video lists the shares Rob mentions in it ("Mentioned in this
  video"); each opens that share in this month's game, after signing in.
  Shares Rob talks about link back to his videos.
- **Live**: Rob's upcoming and current live streams. A "Rob is live" banner
  shows across the whole site while he's on air. Visitors can add a stream to
  their calendar, and members can ask for a reminder email.
- **Courses**: Rob's video courses (7 courses, 94 lessons, copied from the
  Consortium Private Wealth website). Members tick off lessons as they go and
  see their progress. Courses need an account.

Every Learn page ends with the licensee's general advice warning.

## Emails

The site sends these emails:

- **Account**: sign-in codes, welcome, account deleted, and invitations from a
  friend. These can't be turned off.
- **Trades**: an order filled, cancelled or expired; a share split; a
  dividend paid.
- **Games**: a game started, a game ends in 3 days and in 1 day, final results,
  and a referral bonus earned.
- **Community**: an answer to your question, and new courses.
- **Live streams**: Rob has booked a stream, and a reminder when it starts.
- **News from Rob**: newsletters, only to people who opted in.

Players choose which kinds they get on the **Settings** page, and every email
except account emails has a one-click unsubscribe link.

## Account settings

On **Settings** a player can change their display name and avatar, choose
which emails they get, download a copy of all their data, and delete their
account. **Notifications**, under the bell at the top, list everything that
happened to them in the games; the bell shows how many are unread.

**Connect your AI** (`/settings/api`, linked from Settings) lets a player's
own AI check their portfolio and trade for them in this month's game. Claude
and ChatGPT add the site as a connector at `/mcp` and the player signs in on
a consent page, choosing read only or trading. Agents and scripts (Claude
Code, Codex, a cron job) use a personal API key with the JSON API at
`/api/v1`; the page shows the key once, with a prompt to paste that points
the agent at `/agents.md`. Players can revoke keys and disconnect apps there.
The site never runs an AI to choose trades: the player's AI does. Trades made
this way are marked "via AI" in History, and players who use it get a robot
beside their name on the leaderboard (and on their trades in Activity), which
links to `/ai`. `/ai` explains it all, with example prompts; Help and each
game's rules link to it too. Admins can turn AI trading off for a game.

The footer links to About, Help, Contact, Privacy and Terms. The Contact form
needs an account, because everything sent through it while it was open to the
public was spam.

## Admin tools

Admins (Rob and the site team) get an **Admin** menu with:

- **Games**: change a game's name, description, player limit, starting money,
  currency, fee per trade, currency fee (default 0.5%), referral bonus, interest rate, and whether limit
  orders, short selling and after-hours trading are allowed. A game can also be
  cancelled.
- **Picks**: choose and order Rob's Picks, with a note on each.
- **Users**: everyone who has signed up, with their games, trades and last
  visit. Admins can make someone an admin, or block them (a blocked person
  can't sign in, gets no email, and disappears from leaderboards and the lobby).
- **Prices**: whether share prices and the exchange rate are up to date.
- **Email**: write a newsletter, send a test to yourself, then send it to
  everyone who opted in.
- **Courses**: build, edit, publish and remove courses and lessons.
- **Learn**: sync videos, add a Facebook or Instagram video by pasting its link,
  edit topics, hide or pin videos, turn off a video's share links, schedule
  live streams, and connect TikTok.
- **Messages**: read and delete Contact form messages.
- **Lobby**: the lobby messages moderation blocked, with how many are waiting.
  For each one an admin can **Unblock** it (it's posted for everyone), delete it,
  or **Block user** (the author is blocked as on Users, and their messages leave
  the lobby). Any of the three clears it from the count.

## What was cleaned up on 31 October 2026

The site was reviewed for parts that were hidden, unfinished or no longer used.

### Removed

- **Spreadsheet downloads of a game's trades and portfolio history.** Nothing on
  the site linked to them any more. The **Export My Data** button on Settings
  still gives players all their data.
- **Unused game settings**: margin trading, interest on borrowing, stop losses,
  part shares, and minimum and maximum share prices. They could be stored but
  never changed how a game played, and admins couldn't see or set them.
- **Starting shares and game templates**: left over from before monthly games.
  Nothing could set them up.
- **The Markets choice on the admin game page.** It still offered "ASX only",
  which would have given players nothing to buy. The rules page no longer shows
  a Markets line.
- **Google profile photos**, from when people signed in with Google, and the
  **Use Google Photo** button on the Profile page. Players now see the avatar
  they chose, or the default one.
- **Every mention of dividends**, because the game didn't pay them then: the
  Terms said they were credited to players, the email settings mentioned
  dividend emails, and each share's page listed its dividend figures and dates.
  Dividends have been paid since 2 November 2026 (see Extras above).
- **Old web addresses** that only redirected visitors from the old site
  (game.mchugh.au), which has been retired.
- Behind-the-scenes code that nothing used.

### Fixed

- **Four badges could never be earned**: Champion, Podium Finish, Social Player
  and Market Watcher. They now work. Champion and Podium Finish are given out
  when each monthly game finishes, starting with the first game that finishes
  after this change goes live.
- **Badge avatars couldn't be chosen even after earning the badge.** Wizard and
  On Fire were refused for players who had earned them. All four badge avatars
  now unlock properly.
- **All trades now shows each player's avatar**, as the Lobby already did.

## What was cleaned up on 3 November 2026

Prod logs from 30 October to 3 November showed about 24 signed-in visitors.
Pages that only 3 to 5 of them opened (most likely the team) were removed or
folded into others:

- **Analytics**, **Compare** and **Benchmark** were deleted. Benchmark only
  compared a portfolio with holding cash. The sector breakdown moved to the
  home page.
- The **Watchlist** page became the Watchlist button on the Stocks page.
- The **Profile** page became Change avatar in Settings.
- **Activity** moved under Ranks; **Orders** and **Rules** moved into the
  menu under the player's picture, with waiting orders also on the home page.
- The stock page's 50 or so boxes of company figures became eight key figures
  and five folded groups. Phone number, fiscal year end, most recent quarter,
  share type, exchange, revenue and cash per share, float, shares sold short
  and the last split were dropped.

## Things still to decide or do

- **Rob, and his lawyer, to check AI trading**: the new paragraph in Terms
  section 10, the wording on `/ai`, and whether Consortium Private Wealth is
  comfortable with players' own AI trading in the game at all.

- **Rob to check the Terms change**: section 7 says again that dividends are
  credited to players (from 2 November 2026, when the game started paying
  them), and that a short pays them.
- **Rob to check the two 2018 super courses** (Beginners Guide to Super and How
  Super Works). They quote 9.5% employer super, which is now 12%.
- **Rob to delete two unused YouTube live streams.** YouTube still lists them
  as upcoming, though neither ever ran: "Investor Motivation Live Stream" (set
  up 8 January 2026 with no start time,
  youtube.com/watch?v=y874woJzPt8) and "Welcome to our live show!" (booked
  for 29 July 2026, youtube.com/watch?v=mgemRYquSoM). The site now hides
  streams like these by itself, but deleting them in YouTube Studio tidies the
  channel too.
- **Weekly summary email and "we miss you" email**: planned, not built. The
  Weekly summary choice stays hidden on Settings until the email exists.
- **TikTok**: waiting on TikTok to approve the app.
- **Players who already qualify** for Social Player or Market Watcher get the
  badge the next time they join a game or add to a watchlist, not straight
  away.
