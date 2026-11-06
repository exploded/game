# Capacity

How many players the site can take on the current server, what breaks first,
the limits that protect the server, and how to measure again. Measured on
6 November 2026 with `cmd/loadtest`.

## Summary

- **Since the fixes of 6 November 2026, 10,000 players fit.** At 10,000
  players the site served about 120 requests a second before pages slowed,
  about 2,400 players on the site at once, against the 500–1,000 the busiest
  time would bring. Before the fixes it managed about 10 (about 200 at once)
  and fell over. See [Fixes made](#fixes-made).
- **Before the fixes**, a Linode the same as production served about 75
  requests a second at 1,000 players and about 10 at 10,000; the PC matched
  it, and the measurements below are mostly from before the fixes.
- **The limit is the database connection, not the server.** The app does all
  its database work through one SQLite connection. In every test it used no
  more than 1.3 of its 2 CPUs, and memory stayed low until requests piled up.
  More CPU or RAM wouldn't help much.
- **What still grows with players** is revaluing every portfolio every 2
  minutes during the US session: 7.7 seconds at 10,000 players. It's the
  next fix, under [Fixes, for later](#fixes-for-later).
- **Not affected by players:** Twelve Data credits (per stock), the news
  filter, disk space and backups.
- **New limits** keep an overload from taking down the site or the other apps
  on the box: a cap on requests in flight (quick "busy" page instead of pages
  that never load), and memory and CPU limits for the `game` service.

## The server

Linode 4 GB: 2 shared vCPUs (AMD EPYC 7713), 3.9 GB RAM, 79 GB disk. It's
shared with 18 other apps and Caddy. On 6 November 2026 it had about 2.8 GB
of memory free and 68 GB of disk free; `game` used 80 MB and `game.db` was
329 MB, almost all stock prices.

## Today's traffic

From Caddy's access log and the app's request log, 5–6 November 2026:

- 2,536 requests a day from 145 addresses; busiest minute 149 requests.
  Most traffic is crawlers on the public Learn pages.
- 12 players in November's game.
- Median response 2.3 ms, p95 71 ms, worst 1.4 s.
- An active player makes about 3 game requests per 10 minutes; the busiest
  made 15. The capacity figures below assume a busy player at 3 a minute
  (0.05 a second), so 1 request a second is about 20 players on the site.

## How it was measured

`cmd/loadtest` (never deployed) does three things:

1. **`seed`** adds made-up players to a copy of `game.db`, with a month of
   what players leave behind: 60% have 5–15 holdings and the trades that
   bought them, some have open orders, and everyone has daily snapshots,
   interest, notifications and a session. Half the holdings are Rob's Picks
   or the Mag 7. Addresses are `@example.invalid`.
2. **`jobs`** times the background work on the copy, on one connection and
   2 CPUs, as in production.
3. **`run`** drives a test server on the copy at a fixed rate, stepping up
   every minute, and stops once p95 passes 2 s. Each request is a page from
   a phone visit:

   | Page | Share | Page | Share |
   |---|---|---|---|
   | Stock page | 18% | Leaderboard | 6% |
   | Price poll (htmx) | 15% | Video | 5% |
   | Game home | 12% | Candles | 4% |
   | Dashboard | 10% | Activity, lobby, Learn, questions | 3% each |
   | Stocks list | 8% | Trade (htmx POST) | 3% |
   | Stocks chip (htmx) | 5% | Search (htmx) | 2% |

The test server ran twice, revaluing every 2 minutes (`LOADTEST_REVALUE=1`)
and with no access to Twelve Data or other services:

- **On James's PC** (Ryzen 7 3700X), held to 2 logical CPUs with
  `GOMAXPROCS=2`, at 100, 1,000, 3,000 and 10,000 players. The driver ran on
  the other CPUs.
- **On a temporary Linode 4 GB** in Sydney, the same plan and CPU as
  production (AMD EPYC 7713, 2 shared vCPUs, Debian 13), at 1,000 and 10,000
  players. The driver ran on the PC through an SSH tunnel, whose encryption
  stands in for Caddy's TLS. The tunnel added about 42 ms to each request the
  driver timed, so the Linode figures include it. CPU steal from neighbours
  averaged 0.1%.

The Linode matched the PC at both sizes, so the 3,000-player PC figure stands
for the Linode too. Production also runs Caddy and 18 other apps on the same
2 vCPUs. They're idle almost all the time (load average 0.00 on
6 November 2026), but leave some headroom below the knee.

## Results

The knee is the highest rate at which p95 stays under 500 ms. Past it,
response times climb within a minute: on the PC at 1,000 players, 70
requests a second gave a p95 of 331 ms and 80 gave 1.7 s.

| Players | Knee on the PC | Knee on the Linode | Players on at once |
|---|---|---|---|
| 100 | about 75 a second | not measured | about 1,500 |
| 1,000 | about 70 a second | about 75 a second | about 1,500 |
| 3,000 | about 40 a second | not measured | about 800 |
| 10,000 | under 10 a second | about 10 a second | about 200 |

On the Linode at 1,000 players, 80 requests a second gave a p95 of 468 ms,
including the tunnel. At 10,000 players, 10 a second gave 309 ms and
15 gave 682 ms.

If 5–10% of players are on at the busiest time, 3,000 players need 8–15
requests a second (inside the knee) and 10,000 players need 25–50 (far
past it).

**Pages at 10 requests a second** (p50, ms):

| Page | 100 players | 1,000 | 3,000 | 10,000 |
|---|---|---|---|---|
| Dashboard | 3 | 9 | 34 | 241 |
| Game home | 5 | 11 | 37 | 371 |
| Leaderboard | 4 | 19 | 68 | 345 |
| Stock page | 20 | 19 | 19 | 21 |
| Learn | 85 | 80 | 83 | 119 |
| Questions | 73 | 69 | 70 | 124 |

The dashboard, game home and leaderboard all run the leaderboard query, which
grows faster than the player count. The stock page and Learn pages cost the
same at any size; they're slower at 10,000 only because they wait behind the
others for the connection.

**Background jobs** (PC, median):

| Job | When | 100 | 1,000 | 3,000 | 10,000 |
|---|---|---|---|---|---|
| Leaderboard query | Each dashboard, game home, Ranks | 0.6 ms | 7 ms | 28 ms | 158 ms |
| Revalue portfolios | Every 2 min, US session | 69 ms | 0.65–1.25 s | 2.0 s | 7.4 s (up to 9.3 s) |
| Process limit orders | Every 2 min, US session | 0 | 10 ms | 26 ms | 90 ms |
| Pay interest | Daily, just after midnight | 3 ms | 28 ms | 86 ms | 309 ms |
| Queue a game email to everyone | Start, ending soon, results | 20 ms | 0.2 s | 0.7 s | 2.4 s |
| Backup snapshot (`VACUUM INTO`) | Daily, 3 am | 2.1 s | 2.4 s | 2.2 s | 2.7 s |

On the Linode the jobs took about the same: the leaderboard query 7 ms and
136 ms, and revaluation 0.83 s and 8.1 s, at 1,000 and 10,000 players. Only
the backup snapshot, which is mostly disk, was slower: 5.5 s and 4.3 s.

Revaluation runs during the US session, 1:30 am to 8 am in Sydney, while
most players are asleep, so it mostly competes with crawlers and AI apps. It
holds the connection in thousands of short statements rather than one long
one, so pages slow down while it runs but don't stop.

**Memory:** 50–170 MB in normal use. When requests piled up past the knee on
the PC (before the throttle), waiting requests took it to 650–900 MB. With
the throttle on, the Linode peaked at 172 MB at 1,000 players and 256 MB at
10,000.

**CPU:** never more than 1.3 of the 2 CPUs on either machine, even far past
the knee. The single database connection is the bottleneck.

## What breaks first

1. **The leaderboard query** (fixed; see [Fixes made](#fixes-made)). It
   returned every ranked player, with an `EXISTS` over each player's trades
   and orders, on every dashboard, game home and Ranks view. At 10,000
   players that was 158 ms of the connection per view, and those pages are
   28% of requests.
2. **Revaluation** (`market.RevaluePortfolios`). A price lookup and an
   UPDATE for every holding, then 3 statements for every player, each its
   own commit: about 190,000 statements every 2 minutes at 10,000 players.
3. **Sign-in emails behind bulk email** (fixed; see
   [Fixes made](#fixes-made)). The outbox sends 10 a second. A game email to
   3,000 players takes 5 minutes to send, and to 10,000 players about 17
   minutes. Until 6 November 2026 the outbox sent oldest first, so a sign-in
   link queued behind one could arrive after it expired (15 minutes).

## Limits

### Requests in flight (`cmd/server/main.go`)

`handler.Throttle` (`internal/handler/throttle.go`) works on at most **8
requests at once**. Up to **64 more wait**, each for up to **3 seconds**.
Past that, a request gets a 503 with `Retry-After: 30` straight away: a page
load shows a small "The Wealth Lab is busy" page, and an htmx request shows a
toast. Each request that gets a place has a **50-second deadline**, under the
server's 60-second `WriteTimeout`, so one stuck waiting for the database
gives up its place. Refusals log a warning at most once a minute, so they
reach monitor.

- Why 8: requests take turns on the one connection; a few at once keeps the
  2 CPUs busy rendering pages while one works in the database.
- Why 64 and 3 seconds: at the Linode's 75 requests a second (1,000
  players), 64 waiting is under a second of work; at 10 a second (10,000
  players) it would be 6 seconds, and the 3-second wait cuts that short.
  Waiting longer only delays a refusal.
- Not capped: `/static/` and `/favicon.ico` (cheap, cached), `/health` (the
  uptime check), and `/mcp` (AI apps hold it open for minutes and it has its
  own limit of 30 requests a minute per player).

**With the throttle** (PC, 1,000 players):

| Rate | Refused (503) | Served a second | p95 of the rest | Peak memory |
|---|---|---|---|---|
| 40 a second | 0 | 40 | 126 ms | |
| 80 a second | 1% | 79 | 953 ms | |
| 120 a second | 27% | 88 | 1.05 s | |
| 160 a second | 45% | 88 | 1.10 s | 240 MB |

Below the knee it changes nothing. Past it, the server keeps serving at its
maximum (about 88 a second on the PC), the requests it accepts finish in
about a second, there are no errors, and `/health` stays under 120 ms.
Without it, 80 a second gave a p95 of 1.7–4.6 s and memory reached 650 MB;
at 10,000 players, 20 a second gave errors, a p95 of 24 s and failed health
checks, against 9% refused and a p95 of 3.8 s with it. On the Linode at
10,000 players, 25 a second had 5% refused, a p95 of 3.4 s for the rest, no
errors, and `/health` under 320 ms.

### The `game` service (`scripts/deploy-game`)

Each deploy writes `/etc/systemd/system/game.service.d/limits.conf`
(`scripts/server-setup.sh` writes the same on a new server):

| Setting | Value | Why |
|---|---|---|
| `MemoryHigh` | 512M | The kernel slows the app down past this. Normal use is 50–170 MB. |
| `MemoryMax` | 768M | The kernel stops the app past this, rather than squeeze the other apps; systemd restarts it. |
| `GOMEMLIMIT` | 400MiB | Go collects garbage harder before reaching `MemoryHigh`. |
| `CPUQuota` | 150% | At most 1.5 of the 2 vCPUs, leaving half for Caddy and the other apps. The app used at most 1.3 in the tests. |

Check them on the server:

```
ssh linode-claude 'systemctl show game -p MemoryHigh -p MemoryMax -p CPUQuotaPerSecUSec -p Environment'
```

### Limits that aren't code

- **Email:** 10 a second, and 50,000 a day from SES. 10,000 players is
  about 12,000–15,000 emails on a game's busiest day.
- **Twelve Data:** intraday quotes use about 520 of the 600 credits a minute
  the app allows itself (of 610 on Venture). That depends on the number of
  stocks, not players.
- **Lobby moderation:** each post is one Claude Haiku call. Players are
  limited to 5 a minute and 100 a day, but there's no overall budget. If the
  API fails or rate-limits, messages post unchecked.
- **Disk:** a month of 10,000 made-up players added 33 MB to `game.db`
  (about 3.3 KB a player), plus about 5 KB for each email kept in the
  outbox for 90 days. With 68 GB free, disk isn't a concern for years.
- **Backups:** the snapshot took 2–3 seconds at every size and runs on its
  own connection.

## Fixes made

Made on 6 November 2026, after the measurements above:

- **Learn pages.** `ListTopics`, on every Learn page, counted each topic's
  videos separately, so SQLite walked every visible video once per topic
  (52 ms with 1,700 videos). It now counts them in one pass (8 ms). On the
  same data, `/learn` went from 82 to 23 ms, `/learn/questions` from 72 to
  9 ms, and a topic page from 76 to 15 ms.
- **Stocks list cache.** Once the cached rows were a minute old, the next
  request rebuilt them while holding the lock (about 100 ms in production;
  since 4 November its slowest 10% took 90 ms and up). Now that request gets
  the old rows and starts the rebuild in the background (`screenRows`). Only
  the first view after a restart, or after Rob's Picks change, waits.
- **Sign-in email first.** The outbox sends `account` email (sign-in links
  and codes) before anything else (`ListDueEmails`), so a link waits at most
  for the batch being sent, about 5 seconds, not behind a game email to
  every player.

- **Leaderboard query.** Every dashboard, game home, Ranks and API view
  loaded the whole board, looking through each player's trades and orders
  to see who had traded and who used an AI: 128–158 ms at 10,000 players.
  Now `participants.traded` and `uses_ai`, kept by triggers
  (`sql/triggers.sql`), answer that; a player's place is a count
  (`CountRanked`, `GetPlayerAhead`: 2 ms at 10,000), and Ranks shows 100 a
  page (`ListLeaderboard`: 1 ms) with the player's own row added when it's
  on another page. At 10,000 players the PC served 80 requests a second
  with a p95 of 61 ms and no errors, where before it couldn't manage 10.
  The knee moved from under 10 to about 120 requests a second (p95 192 ms);
  at 140 the throttle turned 6% away and the rest stayed under 0.7 s. That
  run had the Learn and stocks list fixes too, but they don't depend on the
  player count.

## Fixes, for later

In order of payoff.

1. **Revaluation.** Look each stock's price up once (about 520) instead of
   once per holding, write in transactions of a few hundred players, and
   skip holdings whose price hasn't changed. Expect seconds to become well
   under a second at 10,000 players.
2. **Stock page.** Each view reads the stock's whole history, up to 5,000
   rows (10 ms), and the candles read it again. Cache the line data per
   stock for the day.
3. **Queries that scan whole tables**, which are fine at these sizes but grow
   with time: `ExpireOldLimitOrders`, `DeleteOldEmails` and
   `CountLobbyMessages`. Revisit them past about 100,000 rows.

Also noticed, not about capacity: portfolio snapshots are dated in UTC, but
the month chart picks them by the game's local dates. A revaluation in the
first 11 hours of the 1st (Sydney time) is dated the previous month, so the
chart leaves it off.

## Measuring again

Run everything on the PC, in a folder of its own: never in the repo, which
has a `.env` with the production keys. `loadtest` refuses a database with a
`.env` beside it, and `run` refuses `thewealthlab.au`. Never load-test
production: the box is shared with the other apps.

PowerShell on your PC, in `C:\Projects\go\game`, to build the tools and copy
the database:

```powershell
New-Item -ItemType Directory -Force C:\Temp\loadtest
go build -o C:\Temp\loadtest\loadtest.exe ./cmd/loadtest
go build -o C:\Temp\loadtest\game-test.exe ./cmd/server
sqlite3 game.db ".backup 'C:/Temp/loadtest/base.db'"
```

PowerShell on your PC, in `C:\Temp\loadtest`, to seed 3,000 players and time
the jobs (the jobs change the copy, so they get a copy of their own):

```powershell
Copy-Item base.db game.db
.\loadtest.exe seed -db game.db -players 3000 -out players.json
Copy-Item game.db jobs.db
.\loadtest.exe jobs -db jobs.db
```

PowerShell on your PC, in `C:\Temp\loadtest`, to start the test server
offline on 8999, held to 2 logical CPUs and revaluing every 2 minutes:

```powershell
$env:PORT = '8999'; $env:BIND_ADDR = '127.0.0.1'; $env:TIMEZONE = 'Australia/Sydney'
$env:LOADTEST_REVALUE = '1'; $env:GOMAXPROCS = '2'
$env:HTTPS_PROXY = 'http://127.0.0.1:9'; $env:HTTP_PROXY = 'http://127.0.0.1:9'
$server = Start-Process .\game-test.exe -PassThru -WindowStyle Hidden -RedirectStandardError server.log
$server.ProcessorAffinity = [IntPtr]0xC
```

PowerShell on your PC, in `C:\Temp\loadtest`, in a new window (so the proxy
settings above don't apply), to drive it:

```powershell
.\loadtest.exe run -base http://127.0.0.1:8999 -players players.json -rates 10,20,30,40,50,60,70,80 -out run.json
```

Afterwards, in the first window, stop the server and delete the copies:

```powershell
Stop-Process -Id $server.Id
Remove-Item game.db*, jobs.db*
```
