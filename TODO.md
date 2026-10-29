# To do

Ideas and requests not yet built. Move an item to `FEATURES.md` once it ships.

## Requests from Rob (2026-10-29)

- [ ] **Answer Rob's open questions for email:** sender name and postal address
  for newsletters (`NEWS_SENDER`), reply-to address (`NEWS_REPLY_TO`). See
  `docs/emails.md`.
- [ ] **Weekly summary and "we miss you" emails** (D1 and N2 in
  `docs/emails.md`). The Weekly summary setting is hidden until D1 exists.

## From the whole-game review (2026-10-29)

Highest impact for the effort first. Recurring games are already fixed.

- [ ] **Invite links.** "Generate Invite Link" never shows the link
  (`ListGameInvites` is unused). Show it with a copy button, and send a
  signed-out visitor back to `/invite/{code}` after sign-in. `InviteJoin`
  ignores `expires_at` and `max_participants` and joins on a GET.
- [ ] **An always-open official game.** A monthly public recurring game Rob
  runs, so a new player always has a game to join.
- [ ] **First-trade onboarding.** After `/welcome`, join the official game and
  land on Stocks, with a short checklist. Today it's about 7 steps.
- [ ] **Hide game settings that do nothing:** margin, stop loss, fractional
  shares, min/max price, interest rates and "Player Portfolios" are stored but
  never enforced. Default limit orders to on. Template games are created
  private, with blank portfolio visibility (`gametemplates.go:53`).
- [ ] **Enforce private games.** `ListActiveGames` lists them and `GameJoin`
  lets anyone in.
- [ ] **Achievements that never fire:** Champion, Podium Finish, Social Player,
  Market Watcher. Return badges are only checked after a market trade.
- [ ] **Stocks page:** show last price and day change, a sector filter and top
  movers.
- [ ] **Moderation:** hide chat messages, suspend users, link
  `/admin/messages` from the Admin page and email contact-form messages.
- [ ] **Promote courses and Questions** on the dashboard and in emails.
- [ ] **Live chat:** poll every 15 seconds and show relative times.
- [ ] **Benchmark against the S&P 500** rather than a flat line.
- [ ] **Shorter nav:** fold Templates, History and Achievements into other pages.
- [ ] **Real splits and dividends from Twelve Data**, so holdings don't break when
  a real split happens.
- [ ] **Small fixes:** the leaderboard rank skips hidden players; chat cuts
  messages at 500 bytes, not characters (can split an emoji); Profile still
  has "Use Google Photo"; Help says "New Game" for the "Create Game" button.

## Follow-ups from the terms review (2026-10-29)

- [ ] **Check who runs StockGame.** Section 2 of the terms
  (`templates/pages/legal/terms.html`) names Local IT Help as the operator.
  Check with Rob whether it should name him instead.
- [ ] **Admin tool to remove chat messages.** Section 9 of the terms says we can
  remove chat messages, but there's no way to do it yet. Add a delete or hide
  action to `internal/handler/chat.go` for admins.
- [ ] **Update the ASX wording.** The About page (`templates/pages/legal/about.html`),
  the create-game page (`templates/pages/games/create.html`) and the game
  templates list (`templates/pages/templates/list.html`) say you can trade
  ASX 200 stocks. While
  the ASX licence is pending, the game offers US-listed Australian companies
  instead (`AusUS` in `internal/market/symbols.go`). Update the wording when
  that change ships, and again if the ASX licence comes through.
