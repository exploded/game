package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// A small OAuth 2.1 authorisation server, so Claude, ChatGPT and other MCP
// clients can connect as a player: the player signs in, picks read only or
// trading on the consent page, and the app gets an access token for /mcp.
// Apps register themselves (RFC 7591) as public clients and must use PKCE
// (S256). Access tokens last an hour; refresh tokens 60 days, replaced on
// every use. Tokens are stored as hashes in api_tokens, like API keys.

const (
	accessTokenLife  = time.Hour
	refreshTokenLife = 60 * 24 * time.Hour
	oauthCodeLife    = 10 * time.Minute
)

// oauthScopes are the scopes a token can have. offline_access is asked for
// by clients that want a refresh token, which every connection gets.
var oauthScopes = []string{"read", "trade"}

// mcpResource is the MCP server's URL, which the protected resource
// metadata must name exactly as players enter it.
func mcpResource(r *http.Request) string { return siteURL(r) + "/mcp" }

// ResourceMetadataURL is where auth.RequireToken's 401 sends MCP clients to
// find how to sign in.
func ResourceMetadataURL(r *http.Request) string {
	return siteURL(r) + "/.well-known/oauth-protected-resource"
}

// OAuthProtectedResource is the RFC 9728 metadata for /mcp and /api.
func (h *Handler) OAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 mcpResource(r),
		"resource_name":            "The Wealth Lab",
		"authorization_servers":    []string{siteURL(r)},
		"scopes_supported":         oauthScopes,
		"bearer_methods_supported": []string{"header"},
		"resource_documentation":   siteURL(r) + "/ai",
	})
}

// OAuthServerMetadata is the RFC 8414 authorisation server metadata.
func (h *Handler) OAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := siteURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"scopes_supported":                      append(slices.Clone(oauthScopes), "offline_access"),
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"service_documentation":                 base + "/ai",
	})
}

// oauthError answers the token and registration endpoints' errors in the
// RFC 6749 shape.
func oauthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// validRedirect accepts an https redirect URI, or an http one on a loopback
// address for a desktop app such as Claude Code.
func validRedirect(raw string) bool {
	if len(raw) > 500 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" || u.User != nil {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return isLoopback(u.Hostname())
	}
	return false
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// redirectAllowed reports whether got is one of a client's registered
// redirect URIs. A loopback URI matches on any port (RFC 8252 7.3), since a
// desktop app picks a free port each time.
func redirectAllowed(registered []string, got string) bool {
	if slices.Contains(registered, got) {
		return true
	}
	g, err := url.Parse(got)
	if err != nil || g.Scheme != "http" || !isLoopback(g.Hostname()) {
		return false
	}
	for _, reg := range registered {
		u, err := url.Parse(reg)
		if err == nil && u.Scheme == g.Scheme && u.Hostname() == g.Hostname() && u.Path == g.Path && u.RawQuery == g.RawQuery {
			return true
		}
	}
	return false
}

// OAuthRegister registers an app (RFC 7591 dynamic client registration).
func (h *Handler) OAuthRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RedirectURIs []string `json:"redirect_uris"`
		ClientName   string   `json:"client_name"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "Send the registration as JSON")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 10 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "Give between 1 and 10 redirect_uris")
		return
	}
	for _, u := range req.RedirectURIs {
		if !validRedirect(u) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "Redirect URIs must be https, or http on localhost: "+u)
			return
		}
	}
	if req.AuthMethod != "" && req.AuthMethod != "none" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "Only public clients (token_endpoint_auth_method none) with PKCE are supported")
		return
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "An AI app"
	}
	if utf8.RuneCountInString(name) > 60 {
		name = string([]rune(name)[:60])
	}
	id, err := auth.RandomHex(16)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "Try again")
		return
	}
	uris, _ := json.Marshal(req.RedirectURIs)
	if err := h.q.CreateOAuthClient(r.Context(), db.CreateOAuthClientParams{ID: id, Name: name, RedirectUris: string(uris)}); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "Try again")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  id,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                name,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// authRequest is an authorisation request's parameters, from the app's link
// to the consent page and back from its form.
type authRequest struct {
	Client        db.OauthClient
	RedirectURI   string
	State         string
	Challenge     string
	Scope         string // what the app asked for; the player chooses on the page
	RedirectHost  string
	LoopbackOnly  bool // every registered redirect is on this computer
	AskedForTrade bool
}

// parseAuthRequest checks an authorisation request. A bad client or
// redirect URI is shown to the player (redirect is ""), since sending them
// to an unchecked address would be an open redirect; anything else goes
// back to the app as an error.
func (h *Handler) parseAuthRequest(r *http.Request, v url.Values) (ar authRequest, redirect, errCode, errDesc string) {
	client, err := h.q.GetOAuthClient(r.Context(), v.Get("client_id"))
	if err != nil {
		return ar, "", "", "This app isn't registered with The Wealth Lab. Try connecting again from the app."
	}
	var registered []string
	_ = json.Unmarshal([]byte(client.RedirectUris), &registered)
	ru := v.Get("redirect_uri")
	if ru == "" && len(registered) == 1 {
		ru = registered[0]
	}
	if !redirectAllowed(registered, ru) {
		return ar, "", "", "This app asked to send you to an address it didn't register. Try connecting again from the app."
	}
	u, _ := url.Parse(ru)
	ar = authRequest{
		Client:       client,
		RedirectURI:  ru,
		State:        v.Get("state"),
		Challenge:    v.Get("code_challenge"),
		Scope:        v.Get("scope"),
		RedirectHost: u.Hostname(),
		LoopbackOnly: true,
	}
	for _, reg := range registered {
		if ru, err := url.Parse(reg); err == nil && !isLoopback(ru.Hostname()) {
			ar.LoopbackOnly = false
		}
	}
	fields := strings.Fields(ar.Scope)
	ar.AskedForTrade = len(fields) == 0 || slices.Contains(fields, "trade")
	switch {
	case v.Get("response_type") != "code":
		return ar, ru, "unsupported_response_type", "Only response_type=code is supported"
	case ar.Challenge == "" || v.Get("code_challenge_method") != "S256":
		return ar, ru, "invalid_request", "PKCE with code_challenge_method=S256 is required"
	case v.Get("resource") != "" && strings.TrimRight(v.Get("resource"), "/") != mcpResource(r) && strings.TrimRight(v.Get("resource"), "/") != siteURL(r):
		return ar, ru, "invalid_target", "Unknown resource"
	}
	return ar, "", "", ""
}

// backToApp sends the browser to the app's redirect URI with params.
func backToApp(w http.ResponseWriter, r *http.Request, redirectURI string, params url.Values) {
	u, _ := url.Parse(redirectURI)
	q := u.Query()
	for k, vs := range params {
		for _, v := range vs {
			if v != "" {
				q.Set(k, v)
			}
		}
	}
	q.Set("iss", siteURL(r)) // RFC 9207
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// allowFormAction lets this page's form end at the app's address: Chrome
// checks the redirect after a form post against form-action too.
func allowFormAction(w http.ResponseWriter, redirectURI string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return
	}
	origin := u.Scheme + "://" + u.Host
	csp := w.Header().Get("Content-Security-Policy")
	w.Header().Set("Content-Security-Policy", strings.Replace(csp, "form-action 'self'", "form-action 'self' "+origin, 1))
}

// OAuthAuthorize shows the consent page. A visitor who isn't signed in signs
// in first and comes back here.
func (h *Handler) OAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	ar, redirect, code, desc := h.parseAuthRequest(r, r.URL.Query())
	if code != "" {
		backToApp(w, r, redirect, url.Values{"error": {code}, "error_description": {desc}, "state": {ar.State}})
		return
	}
	if desc != "" {
		h.render(w, r, "oauth/consent", "", PageData{Title: "Connect an app", Extra: map[string]any{"Problem": desc}})
		return
	}
	if auth.UserFromContext(r.Context()) == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}
	allowFormAction(w, ar.RedirectURI)
	h.render(w, r, "oauth/consent", "", PageData{
		Title: "Connect " + ar.Client.Name,
		Item:  ar,
		Extra: map[string]any{"Query": r.URL.RawQuery},
	})
}

// OAuthDecide is the consent form: Allow issues a code and sends the player
// back to the app; Cancel tells the app they said no.
func (h *Handler) OAuthDecide(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	r.ParseForm()
	v, err := url.ParseQuery(r.FormValue("query"))
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	ar, redirect, code, desc := h.parseAuthRequest(r, v)
	if code != "" {
		backToApp(w, r, redirect, url.Values{"error": {code}, "error_description": {desc}, "state": {ar.State}})
		return
	}
	if desc != "" || user == nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if r.FormValue("decision") != "allow" {
		backToApp(w, r, ar.RedirectURI, url.Values{"error": {"access_denied"}, "error_description": {"The player didn't allow it"}, "state": {ar.State}})
		return
	}
	scope := "read"
	if r.FormValue("access") == "trade" {
		scope = "trade"
	}
	raw, err := auth.RandomHex(32)
	if err != nil {
		h.serverError(w, "oauth code", err)
		return
	}
	if err := h.q.CreateOAuthCode(r.Context(), db.CreateOAuthCodeParams{
		CodeHash:      auth.HashToken(raw),
		ClientID:      ar.Client.ID,
		UserID:        user.ID,
		Scope:         scope,
		RedirectUri:   ar.RedirectURI,
		CodeChallenge: ar.Challenge,
		ExpiresAt:     time.Now().UTC().Add(oauthCodeLife).Format(time.DateTime),
	}); err != nil {
		h.serverError(w, "save oauth code", err)
		return
	}
	backToApp(w, r, ar.RedirectURI, url.Values{"code": {raw}, "state": {ar.State}})
}

// pkceMatches checks a PKCE code_verifier against its S256 challenge.
func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(want), []byte(challenge)) == 1
}

// OAuthToken exchanges a code, or a refresh token, for tokens.
func (h *Handler) OAuthToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Send the form as application/x-www-form-urlencoded")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		h.tokenFromCode(w, r)
	case "refresh_token":
		h.tokenFromRefresh(w, r)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "Use authorization_code or refresh_token")
	}
}

func (h *Handler) tokenFromCode(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	c, err := h.q.TakeOAuthCode(r.Context(), auth.HashToken(f.Get("code")))
	switch {
	case err != nil:
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The code isn't valid, or was used already")
		return
	case expiredAt(c.ExpiresAt):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The code has expired")
		return
	case c.ClientID != f.Get("client_id"):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The code was issued to another app")
		return
	case f.Get("redirect_uri") != "" && f.Get("redirect_uri") != c.RedirectUri:
		oauthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri doesn't match")
		return
	case !pkceMatches(f.Get("code_verifier"), c.CodeChallenge):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier doesn't match the code_challenge")
		return
	}
	client, err := h.q.GetOAuthClient(r.Context(), c.ClientID)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client", "Unknown app")
		return
	}
	grant, err := auth.RandomHex(16)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "Try again")
		return
	}
	h.issueTokens(w, r, c.UserID, client, c.Scope, grant)
}

func (h *Handler) tokenFromRefresh(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	old, err := h.q.GetAPITokenByHash(r.Context(), auth.HashToken(f.Get("refresh_token")))
	if err != nil || old.Kind != "refresh" {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The refresh token isn't valid")
		return
	}
	if old.RevokedAt.Valid {
		// A refresh token used again after it was replaced may have been
		// stolen, so the whole connection goes. A minute's grace covers an
		// app refreshing twice at once.
		if t, err := time.Parse(time.DateTime, old.RevokedAt.String); err == nil && time.Since(t) > time.Minute {
			_ = h.q.RevokeGrant(r.Context(), old.GrantID)
		}
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The refresh token was used already")
		return
	}
	if expiredAt(old.ExpiresAt.String) || old.ClientID.String != f.Get("client_id") {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The refresh token isn't valid")
		return
	}
	if n, err := h.q.RevokeAPIToken(r.Context(), old.ID); err != nil || n == 0 {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The refresh token was used already")
		return
	}
	client, err := h.q.GetOAuthClient(r.Context(), old.ClientID.String)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client", "Unknown app")
		return
	}
	user, err := h.q.GetUser(r.Context(), old.UserID)
	if err != nil || user.BlockedAt.Valid || user.DeletedAt.Valid {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "This account can't connect apps")
		return
	}
	h.issueTokens(w, r, old.UserID, client, old.Scope, old.GrantID.String)
}

// issueTokens makes an access token and a refresh token for one connection
// and answers with them.
func (h *Handler) issueTokens(w http.ResponseWriter, r *http.Request, userID int64, client db.OauthClient, scope, grant string) {
	now := time.Now().UTC()
	issue := func(kind string, life time.Duration) (string, error) {
		token, hash, err := auth.NewToken()
		if err != nil {
			return "", err
		}
		_, err = h.q.CreateAPIToken(r.Context(), db.CreateAPITokenParams{
			UserID:    userID,
			Kind:      kind,
			Name:      client.Name,
			TokenHash: hash,
			Scope:     scope,
			GrantID:   sql.NullString{String: grant, Valid: true},
			ClientID:  sql.NullString{String: client.ID, Valid: true},
			ExpiresAt: sql.NullString{String: now.Add(life).Format(time.DateTime), Valid: true},
		})
		return token, err
	}
	access, err := issue("access", accessTokenLife)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "Try again")
		return
	}
	refresh, err := issue("refresh", refreshTokenLife)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "Try again")
		return
	}
	_ = h.q.TouchOAuthClient(r.Context(), client.ID)
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(accessTokenLife.Seconds()),
		"refresh_token": refresh,
		"scope":         scope,
	})
}

// expiredAt reports whether a stored UTC time has passed. A time that
// doesn't parse counts as passed.
func expiredAt(at string) bool {
	t, err := time.Parse(time.DateTime, at)
	return err != nil || time.Now().After(t)
}
