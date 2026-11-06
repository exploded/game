# The Wealth Lab API for AI agents

The Wealth Lab ({{.Site}}) is a monthly share game with play money. Each
player starts the month with $1,000,000 to invest in US-listed stocks: the
S&P 500, plus some Australian and overseas companies and Bitcoin funds
listed in the US. The biggest portfolio (cash plus holdings) at the end of
the month wins. This API lets you act for one player in this month's game.

This is a game, and nothing here is financial advice. Don't present your own
suggestions as advice from The Wealth Lab.

## Rules for agents

1. **Confirm before trading.** Call `POST /api/v1/orders/preview` and show
   the player what the order will cost, then place it only once they agree.
   Skip the confirmation only when the player has told you to trade without
   asking (for example, a standing instruction in a scheduled task).
2. **Send an `Idempotency-Key` header with every order** (any unique string,
   such as a UUID). If a request times out, retry with the same key: if the
   order went through, you get its answer back instead of a second order.
3. **Respect limits.** {{.Requests}} requests a minute: a `429` means wait
   for the `Retry-After` seconds. The player's AI can place {{.Orders}} orders
   a day (some players have more), counted from midnight in the game's
   `time_zone`; previews, cancels and refused orders don't count. `GET
   /api/v1/game` gives `ai_orders_left_today`. Once it's 0, orders are
   refused until midnight, so don't retry: tell the player.
4. **Read money from `cents` or `text`.** Every amount is
   `{"cents": 123456, "text": "$1,234.56", "currency": "USD"}`. Quantities are
   whole shares.
5. Prices are delayed. While the US market is closed, a market order either
   fills straight away at the last close or waits for the open: `GET
   /api/v1/game` says which.

## Authentication

Send the player's API key in a header on every request:

```
Authorization: Bearer $WEALTHLAB_API_KEY
```

Read the key from the `WEALTHLAB_API_KEY` environment variable. Never put it
in a URL, a file you share, or your reply. Players make keys at
{{.Site}}/settings/api. A read-only key can use everything except placing
and cancelling orders (a `403`).

## Endpoints

Base URL: `{{.Site}}/api/v1`. Responses are JSON. Errors are
`{"error": "message"}` with a status: `401` bad key, `403` read-only key,
`404` not found, `422` the order was refused (the message says why, for
example "Insufficient funds"), `429` too many requests.

| Method | Path | What it does |
|---|---|---|
| GET | `/me` | The player, what the key can do, and the game |
| GET | `/game` | This month's game: dates, fees, rules, whether the US market is open |
| GET | `/portfolio` | Cash, total value, today's move, rank, and every holding |
| GET | `/stocks?q=nvidia` | Search by symbol or name |
| GET | `/stocks?filter=picks&sector=&limit=50` | List stocks by a filter; the response lists the filters and sectors |
| GET | `/stocks/{symbol}` | One stock: price, 30 days of closes, financials, the player's position |
| POST | `/orders/preview` | Work out an order without placing it |
| POST | `/orders` | Place an order |
| GET | `/orders` | Open orders (limit orders, and market orders waiting for the open) |
| DELETE | `/orders/{id}` | Cancel an open order |
| GET | `/history?limit=50` | Trades, interest, dividends and bonuses, newest first |
| GET | `/leaderboard` | The top 20 and where the player stands |

Only players who have made a trade are on the leaderboard. Until the player's
first trade, `ranked` is `false` and `rank` is `0` in both `/portfolio` and
`/leaderboard`, and `players` counts only those who have traded.

Filters for `/stocks`: `picks` (Rob's Picks, the default), `watch` (the
player's watchlist), `all`, `mag7`,
`tech`, `up` (today's gainers), `down` (losers), `big` (biggest), `active`
(most traded), `aus`, `intl`, `crypto`, `popular` (players' favourites), and
tests of a company's financials: `profit`, `loss`, `fcf`, `margin`, `pe`,
`peg`, `revgrowth`, `earngrowth`, `lowdebt`, `netcash` and `score` (health
score 4 or 5). The response's `filters` list has each one's label.

### Orders

```
POST /api/v1/orders
Authorization: Bearer $WEALTHLAB_API_KEY
Content-Type: application/json
Idempotency-Key: 6f1c2b0e-8a7d-4f0e-9a51-0c2d9a7e3b11

{"symbol": "NVDA", "side": "buy", "quantity": 20}
```

| Field | |
|---|---|
| `symbol` | Required. Find it with `/stocks?q=`. |
| `side` | Required. `buy` or `sell`. Selling more than is held goes short, if the game allows it. |
| `quantity` | Whole shares. Leave out with `close_all`. |
| `type` | `market` (the default) or `limit`. |
| `limit_price` | Limit orders: dollars a share in the stock's currency. A buy fills at or below it, a sell at or above. |
| `expires` | Limit orders, optional: the last day it stays open, `YYYY-MM-DD`. |
| `close_all` | `true` to sell every share held (`side: sell`) or buy back a whole short (`side: buy`). |

The answer (`201`, or `200` for a preview):

```json
{
  "status": "filled",
  "message": "Bought 20 shares of NVDA",
  "symbol": "NVDA", "side": "buy", "quantity": 20,
  "price": {"cents": 18250, "text": "$182.50", "currency": "USD"},
  "value": {"cents": 563271, "text": "$5,632.71", "currency": "AUD"},
  "fee": {"cents": 2816, "text": "$28.16", "currency": "AUD"},
  "total": {"cents": 566087, "text": "$5,660.87", "currency": "AUD"},
  "cash_after": {"cents": 99433913, "text": "$994,339.13", "currency": "AUD"},
  "transaction_id": 123
}
```

`status` is `filled` (done), `queued` (a market order waiting for the US
market to open) or `open` (a limit order waiting for its price). `value`,
`fee`, `total` and `cash_after` are in the game's currency; `price` is in
the stock's. Stocks priced in another currency pay a currency fee on top of
the trade fee.

## MCP

The same tools are on an MCP server at `{{.Site}}/mcp` (streamable HTTP).
Connect with the same `Authorization` header, or sign in with OAuth from
Claude, ChatGPT and other MCP clients.

More: {{.Site}}/ai
