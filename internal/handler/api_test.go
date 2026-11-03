package handler

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// apiServer seeds this month's game (id 1) with Alice (user 1) holding $1M
// and ACME at $10.50, and serves the API, MCP and OAuth routes as main.go
// does. Signed-in pages (the consent page) take their user from the X-User
// header.
func apiServer(t *testing.T) (http.Handler, *db.Queries, *Handler, *sql.DB) {
	t.Helper()
	d := openTestDB(t)
	month := market.MonthOf(time.Now().UTC())
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x', 'Alice', 't1'), (2, 'b@x', 'Bob', 't2')`,
		`INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date, limit_orders, quarter)
		 VALUES (1, 1, 'This Month', 'active', 'USD', '` + month.StartDate() + `', '` + month.EndDate() + `', 1, '` + month.Key() + `')`,
		`INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 100000000, 100000000)`,
		`INSERT INTO stocks (id, symbol, name, market, currency, sector) VALUES (1, 'ACME', 'Acme Corp', 'sp500', 'USD', 'Industrials')`,
		`INSERT INTO stock_prices (stock_id, date, open, high, low, close, volume) VALUES (1, '2026-10-28', 1000, 1100, 900, 1050, 5000)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	setMarketOpen(t, true)
	q := db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)

	r := chi.NewRouter()
	requireToken := auth.RequireToken(q, ResourceMetadataURL)
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(requireToken, h.APILimit)
		h.APIRoutes(r)
	})
	r.With(requireToken, h.APILimit).Handle("/mcp", h.MCPHandler())
	r.Get("/.well-known/oauth-protected-resource", h.OAuthProtectedResource)
	r.Get("/.well-known/oauth-authorization-server", h.OAuthServerMetadata)
	r.Post("/oauth/register", h.OAuthRegister)
	r.Post("/oauth/token", h.OAuthToken)
	r.Get("/agents.md", h.AgentsDoc)
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if id := r.Header.Get("X-User"); id != "" {
					n := int64(1)
					if id == "2" {
						n = 2
					}
					user, _ := q.GetUser(r.Context(), n)
					r = r.WithContext(auth.WithUser(r.Context(), &user))
				}
				next.ServeHTTP(w, r)
			})
		})
		r.Get("/oauth/authorize", h.OAuthAuthorize)
		r.Post("/oauth/authorize", h.OAuthDecide)
		r.Post("/games/{id}/trade/{sid}", h.TradeExecute)
		r.Get("/settings/api", h.APIKeysPage)
		r.Post("/settings/api/keys", h.APIKeyCreate)
	})
	return r, q, h, d
}

// newKey makes an API key for user 1.
func newKey(t *testing.T, q *db.Queries, scope string) string {
	t.Helper()
	token, hash, err := auth.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateAPIToken(t.Context(), db.CreateAPITokenParams{
		UserID: 1, Kind: "key", Name: "test", TokenHash: hash, Scope: scope,
	}); err != nil {
		t.Fatal(err)
	}
	return token
}

func callAPI(srv http.Handler, method, path, token, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return v
}

func cashOf(t *testing.T, q *db.Queries) int64 {
	t.Helper()
	return alice(t, q).CashBalance
}

func TestAPINeedsAValidToken(t *testing.T) {
	srv, q, _, _ := apiServer(t)
	key := newKey(t, q, "read")

	w := callAPI(srv, "GET", "/api/v1/portfolio", "", "")
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Header().Get("WWW-Authenticate"), `resource_metadata="http://example.com/.well-known/oauth-protected-resource"`) {
		t.Errorf("no token: %d %q, want 401 pointing at the metadata", w.Code, w.Header().Get("WWW-Authenticate"))
	}
	if w := callAPI(srv, "GET", "/api/v1/portfolio", "wl_nope", ""); w.Code != http.StatusUnauthorized || !strings.Contains(w.Header().Get("WWW-Authenticate"), "invalid_token") {
		t.Errorf("bad token: %d %q, want 401 invalid_token", w.Code, w.Header().Get("WWW-Authenticate"))
	}

	// A session cookie is no use here.
	req := httptest.NewRequest("GET", "/api/v1/portfolio", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("cookie: %d, want 401", rec.Code)
	}

	if w := callAPI(srv, "GET", "/api/v1/portfolio", key, ""); w.Code != 200 {
		t.Fatalf("good key: %d %s", w.Code, w.Body.String())
	}
	keys, _ := q.ListAPIKeys(t.Context(), 1)
	if n, _ := q.RevokeAPIKey(t.Context(), db.RevokeAPIKeyParams{ID: keys[0].ID, UserID: 1}); n != 1 {
		t.Fatal("couldn't revoke")
	}
	if w := callAPI(srv, "GET", "/api/v1/portfolio", key, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("revoked key: %d, want 401", w.Code)
	}
}

func TestAPIPreviewThenOrder(t *testing.T) {
	srv, q, _, d := apiServer(t)
	key := newKey(t, q, "trade")
	order := `{"symbol": "acme", "side": "buy", "quantity": 10}`

	w := callAPI(srv, "POST", "/api/v1/orders/preview", key, order)
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	p := decode(t, w)
	if p["preview"] != true || p["status"] != "filled" || !strings.Contains(p["message"].(string), "Nothing has been placed") {
		t.Errorf("preview = %v", p)
	}
	if cash := cashOf(t, q); cash != 100000000 {
		t.Errorf("a preview spent cash: %d", cash)
	}

	w = callAPI(srv, "POST", "/api/v1/orders", key, order, "Idempotency-Key", "k1")
	if w.Code != http.StatusCreated {
		t.Fatalf("order: %d %s", w.Code, w.Body.String())
	}
	first := w.Body.String()
	if o := decode(t, w); o["status"] != "filled" || o["message"] != "Bought 10 shares of ACME" {
		t.Errorf("order = %v", o)
	}
	// A retry with the same key gets the same answer and trades once.
	w = callAPI(srv, "POST", "/api/v1/orders", key, order, "Idempotency-Key", "k1")
	if w.Code != http.StatusCreated || w.Body.String() != first || w.Header().Get("Idempotent-Replayed") != "true" {
		t.Errorf("retry: %d %q, want the first answer replayed", w.Code, w.Body.String())
	}
	if h := holding(t, q); h.Quantity != 10 {
		t.Errorf("holding = %d, want 10", h.Quantity)
	}
	if cash := cashOf(t, q); cash != 100000000-10*1050 {
		t.Errorf("cash = %d", cash)
	}
	var source string
	d.QueryRow(`SELECT source FROM transactions`).Scan(&source)
	if source != "api" {
		t.Errorf("transaction source = %q, want api", source)
	}

	pf := decode(t, callAPI(srv, "GET", "/api/v1/portfolio", key, ""))
	holdings := pf["holdings"].([]any)
	if len(holdings) != 1 || holdings[0].(map[string]any)["symbol"] != "ACME" {
		t.Errorf("portfolio holdings = %v", holdings)
	}
	lb := decode(t, callAPI(srv, "GET", "/api/v1/leaderboard", key, ""))
	if you := lb["you"].(map[string]any); you["uses_ai"] != true || you["rank"] != float64(1) {
		t.Errorf("leaderboard you = %v, want rank 1 with the AI badge", you)
	}
}

func TestAPIRefusals(t *testing.T) {
	srv, q, _, d := apiServer(t)
	read := newKey(t, q, "read")
	trade := newKey(t, q, "trade")

	if w := callAPI(srv, "POST", "/api/v1/orders", read, `{"symbol": "ACME", "side": "buy", "quantity": 1}`); w.Code != http.StatusForbidden {
		t.Errorf("read-only key placed an order: %d %s", w.Code, w.Body.String())
	}
	if w := callAPI(srv, "POST", "/api/v1/orders/preview", read, `{"symbol": "ACME", "side": "buy", "quantity": 1}`); w.Code != 200 {
		t.Errorf("read-only key can't preview: %d", w.Code)
	}
	w := callAPI(srv, "POST", "/api/v1/orders", trade, `{"symbol": "ACME", "side": "buy", "quantity": 1000000}`, "Idempotency-Key", "big")
	if w.Code != http.StatusUnprocessableEntity || decode(t, w)["error"] != "Insufficient funds" {
		t.Errorf("too big: %d %s", w.Code, w.Body.String())
	}
	// A refusal isn't kept against the key: a retry is checked again.
	if w := callAPI(srv, "POST", "/api/v1/orders", trade, `{"symbol": "ACME", "side": "buy", "quantity": 1}`, "Idempotency-Key", "big"); w.Code != http.StatusCreated {
		t.Errorf("retry after a refusal: %d %s", w.Code, w.Body.String())
	}
	if w := callAPI(srv, "POST", "/api/v1/orders", trade, `{"symbol": "NOPE", "side": "buy", "quantity": 1}`); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown stock: %d", w.Code)
	}

	// With AI trading off, the API can't trade but the site still can.
	if _, err := d.Exec(`UPDATE games SET ai_trading = 0`); err != nil {
		t.Fatal(err)
	}
	w = callAPI(srv, "POST", "/api/v1/orders", trade, `{"symbol": "ACME", "side": "buy", "quantity": 1}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "turned off") {
		t.Errorf("AI trading off: %d %s", w.Code, w.Body.String())
	}
	req := httptest.NewRequest("POST", "/games/1/trade/1", strings.NewReader("side=buy&quantity=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-User", "1")
	srv.ServeHTTP(httptest.NewRecorder(), req)
	if h := holding(t, q); h.Quantity != 2 {
		t.Errorf("site trade with AI trading off: holding %d, want 2", h.Quantity)
	}
}

func TestAPILimitAndCancel(t *testing.T) {
	srv, q, _, _ := apiServer(t)
	key := newKey(t, q, "trade")
	w := callAPI(srv, "POST", "/api/v1/orders", key, `{"symbol": "ACME", "side": "buy", "quantity": 5, "type": "limit", "limit_price": 9.5}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("limit order: %d %s", w.Code, w.Body.String())
	}
	o := decode(t, w)
	if o["status"] != "open" {
		t.Errorf("status = %v, want open", o["status"])
	}
	id := int64(o["order_id"].(float64))
	var list []map[string]any
	json.Unmarshal(callAPI(srv, "GET", "/api/v1/orders", key, "").Body.Bytes(), &list)
	if len(list) != 1 || list[0]["via_ai"] != true {
		t.Errorf("open orders = %v, want the limit order", list)
	}
	path := "/api/v1/orders/" + strconv.FormatInt(id, 10)
	if w := callAPI(srv, "DELETE", path, key, ""); w.Code != 200 {
		t.Errorf("cancel: %d %s", w.Code, w.Body.String())
	}
	if w := callAPI(srv, "DELETE", path, key, ""); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("cancel twice: %d, want 422", w.Code)
	}
	// Bob can't cancel Alice's orders, or see that they exist.
	w = callAPI(srv, "POST", "/api/v1/orders", key, `{"symbol": "ACME", "side": "buy", "quantity": 5, "type": "limit", "limit_price": 9.5}`)
	id = int64(decode(t, w)["order_id"].(float64))
	token, hash, _ := auth.NewToken()
	q.CreateAPIToken(t.Context(), db.CreateAPITokenParams{UserID: 2, Kind: "key", TokenHash: hash, Scope: "trade"})
	if w := callAPI(srv, "DELETE", "/api/v1/orders/"+strconv.FormatInt(id, 10), token, ""); w.Code != http.StatusNotFound {
		t.Errorf("someone else's order: %d, want 404", w.Code)
	}
}

func TestAPIDailyOrderLimit(t *testing.T) {
	srv, q, _, d := apiServer(t)
	key := newKey(t, q, "trade")
	if _, err := d.Exec(`UPDATE users SET ai_orders_per_day = 2 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	order := `{"symbol": "ACME", "side": "buy", "quantity": 1}`
	left := func() any { return decode(t, callAPI(srv, "GET", "/api/v1/game", key, ""))["ai_orders_left_today"] }

	// Refused orders and previews don't count.
	if w := callAPI(srv, "POST", "/api/v1/orders", key, `{"symbol": "ACME", "side": "buy", "quantity": 1000000}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("too big: %d %s", w.Code, w.Body.String())
	}
	if n := left(); n != float64(2) {
		t.Errorf("after a refusal, %v left, want 2", n)
	}
	for i := range 2 {
		if w := callAPI(srv, "POST", "/api/v1/orders", key, order); w.Code != http.StatusCreated {
			t.Fatalf("order %d: %d %s", i+1, w.Code, w.Body.String())
		}
	}
	if n := left(); n != float64(0) {
		t.Errorf("after 2 orders, %v left, want 0", n)
	}
	w := callAPI(srv, "POST", "/api/v1/orders", key, order)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "placed its 2 orders for today") {
		t.Errorf("over the limit: %d %s", w.Code, w.Body.String())
	}
	if w := callAPI(srv, "POST", "/api/v1/orders/preview", key, order); w.Code != 200 {
		t.Errorf("preview over the limit: %d %s", w.Code, w.Body.String())
	}
	// MCP shares the count.
	ts := httptest.NewServer(srv)
	defer ts.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{key}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "place_order", Arguments: map[string]any{"symbol": "ACME", "side": "buy", "quantity": 1}})
	if err != nil || !res.IsError {
		t.Errorf("MCP order over the limit: %v %+v", err, res)
	}
	if h := holding(t, q); h.Quantity != 2 {
		t.Errorf("holding = %d, want 2", h.Quantity)
	}

	// An admin raising the limit lets the AI trade again today.
	if _, err := d.Exec(`UPDATE users SET ai_orders_per_day = 3 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if w := callAPI(srv, "POST", "/api/v1/orders", key, order); w.Code != http.StatusCreated {
		t.Errorf("after raising the limit: %d %s", w.Code, w.Body.String())
	}
}

func TestAPIStocks(t *testing.T) {
	srv, q, _, _ := apiServer(t)
	key := newKey(t, q, "read")
	list := decode(t, callAPI(srv, "GET", "/api/v1/stocks?q=acme", key, ""))
	if stocks := list["stocks"].([]any); len(stocks) != 1 || stocks[0].(map[string]any)["symbol"] != "ACME" {
		t.Errorf("search = %v", list["stocks"])
	}
	w := callAPI(srv, "GET", "/api/v1/stocks/ACME", key, "")
	if w.Code != 200 {
		t.Fatalf("stock: %d %s", w.Code, w.Body.String())
	}
	if s := decode(t, w); s["price"].(map[string]any)["text"] != "$10.50" {
		t.Errorf("stock = %v", s)
	}
	if w := callAPI(srv, "GET", "/api/v1/stocks/NOPE", key, ""); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown stock: %d", w.Code)
	}
}

// TestOrdersDontLoseCash places orders at once: each must see the cash the
// one before left, or a balance written back whole undoes another order.
func TestOrdersDontLoseCash(t *testing.T) {
	_, q, h, _ := apiServer(t)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := h.PlaceOrder(t.Context(), Order{UserID: 1, GameID: 1, StockID: 1, Side: "buy", Quantity: 1}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if cash := cashOf(t, q); cash != 100000000-10*1050 {
		t.Errorf("cash = %d, want %d", cash, 100000000-10*1050)
	}
	if ho := holding(t, q); ho.Quantity != 10 {
		t.Errorf("holding = %d, want 10", ho.Quantity)
	}
}

// bearer adds an Authorization header to every request.
type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestMCPTools(t *testing.T) {
	srv, q, _, d := apiServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	key := newKey(t, q, "trade")

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{key}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = tool
	}
	for _, n := range []string{"get_game", "get_portfolio", "find_stocks", "get_stock", "preview_order", "place_order", "list_orders", "cancel_order", "get_history", "get_leaderboard"} {
		if names[n] == nil {
			t.Errorf("no %s tool", n)
		}
	}
	if a := names["place_order"].Annotations; a == nil || a.ReadOnlyHint {
		t.Error("place_order is marked read only")
	}
	if a := names["get_portfolio"].Annotations; a == nil || !a.ReadOnlyHint {
		t.Error("get_portfolio isn't marked read only")
	}

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "place_order", Arguments: map[string]any{"symbol": "ACME", "side": "buy", "quantity": 3}})
	if err != nil || res.IsError {
		t.Fatalf("place_order: %v %+v", err, res)
	}
	var source string
	d.QueryRow(`SELECT source FROM transactions`).Scan(&source)
	if source != "mcp" {
		t.Errorf("transaction source = %q, want mcp", source)
	}

	res, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "place_order", Arguments: map[string]any{"symbol": "ACME", "side": "buy", "quantity": 10000000}})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "Insufficient funds") {
		t.Errorf("refused order: %v %+v, want a tool error saying why", err, res)
	}

	res, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_portfolio", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("get_portfolio: %v %+v", err, res)
	}
	if b, _ := json.Marshal(res.StructuredContent); !strings.Contains(string(b), `"symbol":"ACME"`) {
		t.Errorf("portfolio = %s", b)
	}
}

// TestMCPListenStreams checks that MCP's subscriptions/listen stream gets
// through main.go's middleware: the request logger and compression must pass
// flushes on, or the stream's first event never leaves the server.
func TestMCPListenStreams(t *testing.T) {
	srv, q, _, _ := apiServer(t)
	ts := httptest.NewServer(RequestLogger(middleware.Compress(5)(srv)))
	defer ts.Close()
	key := newKey(t, q, "read")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	body := `{"jsonrpc":"2.0","id":1,"method":"subscriptions/listen","params":{"notifications":{"toolsListChanged":true},` +
		`"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	req, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "subscriptions/listen")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); resp.StatusCode != 200 || ct != "text/event-stream" {
		t.Fatalf("listen = %d %s", resp.StatusCode, ct)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.Contains(sc.Text(), "notifications/subscriptions/acknowledged") {
			return
		}
	}
	t.Fatalf("no acknowledgement on the stream: %v", sc.Err())
}

func TestRedirectRules(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://claude.ai/api/mcp/auth_callback": true,
		"http://localhost:3118/callback":          true,
		"http://127.0.0.1/callback":               true,
		"http://evil.com/callback":                false,
		"javascript:alert(1)":                     false,
		"https://a.com/cb#frag":                   false,
	} {
		if got := validRedirect(raw); got != want {
			t.Errorf("validRedirect(%q) = %v", raw, got)
		}
	}
	reg := []string{"http://localhost/callback", "https://claude.ai/api/mcp/auth_callback"}
	for got, want := range map[string]bool{
		"http://localhost:51234/callback":              true,
		"https://claude.ai/api/mcp/auth_callback":      true,
		"http://localhost:51234/other":                 false,
		"https://claude.ai:8443/api/mcp/auth_callback": false,
		"http://127.0.0.1:5000/callback":               false,
	} {
		if redirectAllowed(reg, got) != want {
			t.Errorf("redirectAllowed(%q) = %v", got, !want)
		}
	}
}

func pkcePair() (verifier, challenge string) {
	verifier = strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func postForm(srv http.Handler, path string, form url.Values, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if user != "" {
		req.Header.Set("X-User", user)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestOAuthConnect(t *testing.T) {
	srv, _, _, _ := apiServer(t)
	if m := decode(t, callAPI(srv, "GET", "/.well-known/oauth-protected-resource", "", "")); m["resource"] != "http://example.com/mcp" {
		t.Errorf("resource metadata = %v", m)
	}
	if m := decode(t, callAPI(srv, "GET", "/.well-known/oauth-authorization-server", "", "")); m["registration_endpoint"] != "http://example.com/oauth/register" {
		t.Errorf("server metadata = %v", m)
	}

	if w := callAPI(srv, "POST", "/oauth/register", "", `{"redirect_uris": ["http://evil.com/cb"]}`); w.Code != http.StatusBadRequest {
		t.Errorf("http redirect registered: %d", w.Code)
	}
	w := callAPI(srv, "POST", "/oauth/register", "", `{"client_name": "Claude", "redirect_uris": ["https://claude.ai/api/mcp/auth_callback"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	clientID := decode(t, w)["client_id"].(string)

	verifier, challenge := pkcePair()
	query := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"xyz"}, "scope": {"read trade"},
	}.Encode()

	// Signed out: sign in first, then come back.
	if w := callAPI(srv, "GET", "/oauth/authorize?"+query, "", ""); w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login?next=") {
		t.Errorf("signed out: %d to %q", w.Code, w.Header().Get("Location"))
	}
	w = callAPI(srv, "GET", "/oauth/authorize?"+query, "", "", "X-User", "1")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Connect Claude to The Wealth Lab?") || !strings.Contains(w.Body.String(), "claude.ai") {
		t.Fatalf("consent page: %d", w.Code)
	}
	if csp := w.Header().Get("Content-Security-Policy"); csp != "" && !strings.Contains(csp, "form-action 'self' https://claude.ai") {
		t.Errorf("CSP doesn't let the form end at claude.ai: %q", csp)
	}
	// An unregistered redirect is shown, never followed.
	bad := strings.Replace(query, "claude.ai", "evil.com", 1)
	if w := callAPI(srv, "GET", "/oauth/authorize?"+bad, "", "", "X-User", "1"); w.Code != 200 || !strings.Contains(w.Body.String(), "Couldn't connect") {
		t.Errorf("bad redirect: %d %q", w.Code, w.Header().Get("Location"))
	}

	w = postForm(srv, "/oauth/authorize", url.Values{"query": {query}, "decision": {"allow"}, "access": {"trade"}}, "1")
	loc, _ := url.Parse(w.Header().Get("Location"))
	if w.Code != http.StatusFound || loc.Host != "claude.ai" || loc.Query().Get("state") != "xyz" || loc.Query().Get("code") == "" {
		t.Fatalf("allow: %d to %q", w.Code, loc)
	}
	code := loc.Query().Get("code")

	exchange := func(v url.Values) *httptest.ResponseRecorder { return postForm(srv, "/oauth/token", v, "") }
	if w := exchange(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "code_verifier": {strings.Repeat("x", 50)}}); w.Code != http.StatusBadRequest {
		t.Errorf("wrong verifier: %d", w.Code)
	}
	// The failed try used the code up.
	if w := exchange(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "code_verifier": {verifier}}); w.Code != http.StatusBadRequest {
		t.Errorf("code used twice: %d", w.Code)
	}

	w = postForm(srv, "/oauth/authorize", url.Values{"query": {query}, "decision": {"allow"}, "access": {"trade"}}, "1")
	loc, _ = url.Parse(w.Header().Get("Location"))
	w = exchange(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "client_id": {clientID}, "code_verifier": {verifier},
		"redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}})
	if w.Code != 200 {
		t.Fatalf("exchange: %d %s", w.Code, w.Body.String())
	}
	tok := decode(t, w)
	access, refresh := tok["access_token"].(string), tok["refresh_token"].(string)
	if tok["scope"] != "trade" || tok["token_type"] != "Bearer" {
		t.Errorf("tokens = %v", tok)
	}
	me := decode(t, callAPI(srv, "GET", "/api/v1/me", access, ""))
	if me["access"] != "trade" || me["via"] != "Claude" || me["name"] != "Alice" {
		t.Errorf("me = %v", me)
	}
	if w := callAPI(srv, "GET", "/api/v1/me", refresh, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("a refresh token worked as an access token: %d", w.Code)
	}

	w = exchange(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}})
	if w.Code != 200 {
		t.Fatalf("refresh: %d %s", w.Code, w.Body.String())
	}
	if newRefresh := decode(t, w)["refresh_token"]; newRefresh == refresh {
		t.Error("refresh token wasn't replaced")
	}
	if w := exchange(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}}); w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_grant" {
		t.Errorf("old refresh token reused: %d", w.Code)
	}

	// Cancel tells the app no.
	w = postForm(srv, "/oauth/authorize", url.Values{"query": {query}, "decision": {"deny"}}, "1")
	if loc, _ := url.Parse(w.Header().Get("Location")); loc.Query().Get("error") != "access_denied" {
		t.Errorf("deny went to %q", loc)
	}
}

func TestAgentsDoc(t *testing.T) {
	srv, _, _, _ := apiServer(t)
	w := callAPI(srv, "GET", "/agents.md", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "http://example.com/api/v1") || !strings.Contains(w.Body.String(), "30 requests a minute") || !strings.Contains(w.Body.String(), "can place 12 orders") {
		t.Errorf("agents.md: %d\n%s", w.Code, w.Body.String())
	}
}

func TestAllowFormAction(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("Content-Security-Policy", "default-src 'self'; form-action 'self'; frame-ancestors 'none'")
	allowFormAction(w, "http://localhost:3118/callback?x=1")
	if got := w.Header().Get("Content-Security-Policy"); !strings.Contains(got, "form-action 'self' http://localhost:3118;") {
		t.Errorf("CSP = %q", got)
	}
}

// TestNewKeyShownOnce: the page that makes a key shows it with the prompt
// to paste, and later visits show only its start.
func TestNewKeyShownOnce(t *testing.T) {
	srv, q, _, _ := apiServer(t)
	w := postForm(srv, "/settings/api/keys", url.Values{"name": {"Laptop"}, "access": {"trade"}}, "1")
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Read http://example.com/agents.md") {
		t.Fatalf("make key: %d", w.Code)
	}
	keys, _ := q.ListAPIKeys(t.Context(), 1)
	if len(keys) != 1 || keys[0].Scope != "trade" {
		t.Fatalf("keys = %+v", keys)
	}
	i := strings.Index(body, `<pre id="new-key">`)
	token := body[i+len(`<pre id="new-key">`):][:len(auth.TokenPrefix)+64]
	if auth.HashToken(token) != keys[0].TokenHash {
		t.Error("the key shown isn't the key stored")
	}
	if page := getPage(t, srv, "/settings/api", "1"); strings.Contains(page, token) || !strings.Contains(page, keys[0].Prefix) {
		t.Error("the settings page shows the whole key again, or not its start")
	}
	if w := postForm(srv, "/settings/api/keys", url.Values{"name": {""}, "access": {"trade"}}, "1"); !strings.Contains(w.Body.String(), "Give the key a name") {
		t.Error("a key with no name wasn't refused")
	}
}
