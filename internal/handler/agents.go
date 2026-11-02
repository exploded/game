package handler

import (
	_ "embed"
	"fmt"
	"net/http"
	"text/template"
)

// agents.md tells an AI agent how to use the API. A player pastes a prompt
// pointing their agent at /agents.md (from /settings/api).
//
//go:embed agents.md
var agentsDoc string

var agentsTmpl = template.Must(template.New("agents.md").Parse(agentsDoc))

// Each player's API and MCP limits, quoted in agents.md and on /ai. An admin
// can give a player more orders a day at /admin/users (users.ai_orders_per_day).
const (
	APIRequestsPerMinute = 30
	AIOrdersPerDay       = 12
)

// AgentsDoc serves /agents.md.
func (h *Handler) AgentsDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	agentsTmpl.Execute(w, map[string]any{
		"Site":     siteURL(r),
		"Requests": APIRequestsPerMinute,
		"Orders":   AIOrdersPerDay,
	})
}

// LLMsTxt serves /llms.txt, which points AI tools at agents.md.
func (h *Handler) LLMsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	site := siteURL(r)
	fmt.Fprintf(w, `# The Wealth Lab

> A monthly share game with play money: $1,000,000 to invest in US-listed stocks, biggest portfolio wins. Players can let their own AI check their portfolio and trade for them.

- [API for AI agents](%[1]s/agents.md): how an agent with a player's API key reads their portfolio and places orders
- [MCP server](%[1]s/mcp): the same tools for MCP clients, with OAuth sign-in
- [Connecting your AI](%[1]s/ai): for players
`, site)
}

// AIPage is /ai: how players connect their own AI, with example prompts.
func (h *Handler) AIPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "legal/ai", "", PageData{
		Title: "Trade with your AI",
		Extra: map[string]any{"MCPURL": siteURL(r) + "/mcp", "Site": siteURL(r), "AIOrders": AIOrdersPerDay},
	})
}
