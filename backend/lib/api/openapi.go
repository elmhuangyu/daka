package api

import (
	"encoding/json"
	"net/http"
	"sync"
)

// The OpenAPI document describes only the AI-readable surface: the GET
// endpoints a read-scope ApiToken may call (see readAllowed). It contains no
// secrets and is served without authentication, so an agent can discover the
// API before it holds a token.

type openAPIParam struct {
	Name, In, Description string
	Required              bool
	Type                  string
}

type openAPIOp struct {
	Path, ID, Summary string
	Params            []openAPIParam
	Response          string // description of the 200 body
}

var pItemID = openAPIParam{Name: "id", In: "path", Required: true, Type: "integer", Description: "Item id (from GET /api/items)."}

func pDate(name, desc string, req bool) openAPIParam {
	return openAPIParam{Name: name, In: "query", Required: req, Type: "string", Description: desc + " Format YYYY-MM-DD, in the user's timezone."}
}

func openAPIOps() []openAPIOp {
	return []openAPIOp{
		{Path: "/api/items", ID: "listItems", Summary: "List or search items. Sensitive items are never returned to read tokens.",
			Params: []openAPIParam{
				{Name: "q", In: "query", Type: "string", Description: "Search by name: case-insensitive; every whitespace-separated term (max 8) must appear in the name, in any order."},
				{Name: "kind", In: "query", Type: "string", Description: "habit or event."},
				{Name: "polarity", In: "query", Type: "string", Description: "positive or negative."},
				{Name: "category_id", In: "query", Type: "integer", Description: "Filter by category id."},
				{Name: "project_id", In: "query", Type: "integer", Description: "Only items of this project (see GET /api/projects). Items carry project_id and parent_id (sub-task of another item)."},
				{Name: "archived", In: "query", Type: "string", Description: "false (default), true or all."},
				{Name: "pinned", In: "query", Type: "string", Description: "true or false."},
				{Name: "include", In: "query", Type: "string", Description: "Set to stats to embed last time, counts, quota and a human-readable summary per item."},
			},
			Response: `Object {"items": [Item]}.`},
		{Path: "/api/projects", ID: "listProjects", Summary: "List projects (goals / to-do lists that group items). Sensitive projects are never returned to read tokens.",
			Params: []openAPIParam{
				{Name: "status", In: "query", Type: "string", Description: "active, completed or archived."},
				{Name: "category_id", In: "query", Type: "integer", Description: "Filter by category id."},
			},
			Response: `Array of Project {id, name, description, status, target_date, category_id, emoji, ...}.`},
		{Path: "/api/projects/{id}", ID: "getProject", Summary: "One project. 404 for sensitive projects.",
			Params: []openAPIParam{{Name: "id", In: "path", Required: true, Type: "integer", Description: "Project id (from GET /api/projects)."}}, Response: "Project object."},
		{Path: "/api/items/{id}", ID: "getItem", Summary: "One item with its current rule and options. 404 for sensitive items.",
			Params: []openAPIParam{pItemID}, Response: "Item object."},
		{Path: "/api/items/{id}/stats", ID: "getItemStats", Summary: "Last time, counts per period, quota remaining and a summary sentence for one item. 404 for sensitive items.",
			Params: []openAPIParam{pItemID}, Response: `Object with item_id, name, stats {last_at, days_since_last, counts, quota, maintenance, summary}.`},
		{Path: "/api/checkins", ID: "listCheckins", Summary: "Check-in logs, newest first, cursor-paginated. Logs of sensitive items are never returned.",
			Params: []openAPIParam{
				{Name: "item_id", In: "query", Type: "integer", Description: "Only logs of this item."},
				{Name: "q", In: "query", Type: "string", Description: "Search the check-in note: case-insensitive; every whitespace-separated term (max 8) must appear."},
				pDate("from", "First date, inclusive.", false),
				pDate("to", "Last date, inclusive.", false),
				{Name: "limit", In: "query", Type: "integer", Description: "Page size, 1 to 200."},
				{Name: "cursor", In: "query", Type: "string", Description: "next_cursor of the previous page."},
			},
			Response: `Object {"checkins": [Checkin], "next_cursor": string|null}.`},
		{Path: "/api/view/today", ID: "getToday", Summary: "Items due today with done flags and progress. Sensitive items are omitted.",
			Response: "Object with date, timezone, progress {done,total} and items."},
		{Path: "/api/view/calendar", ID: "getCalendarView", Summary: "Per-day due/done status over a date range (max 93 days). Sensitive items are omitted.",
			Params:   []openAPIParam{pDate("start", "First date.", true), pDate("end", "Last date.", true), {Name: "item", In: "query", Type: "integer", Description: "Only this item."}},
			Response: "Object with days[] (holiday markers, items with due/done/status), freezes[] (without reasons)."},
		{Path: "/api/calendar/holidays", ID: "getHolidays", Summary: "Statutory holidays and make-up workdays of a year.",
			Params:   []openAPIParam{{Name: "year", In: "query", Type: "integer", Description: "Year; defaults to the current one."}},
			Response: "Object with holiday days and published flag."},
	}
}

var (
	openAPIOnce sync.Once
	openAPIBody []byte
)

func buildOpenAPI() []byte {
	paths := map[string]any{}
	for _, op := range openAPIOps() {
		params := []any{}
		for _, p := range op.Params {
			params = append(params, map[string]any{
				"name": p.Name, "in": p.In, "required": p.Required, "description": p.Description,
				"schema": map[string]any{"type": p.Type},
			})
		}
		get := map[string]any{
			"operationId": op.ID, "summary": op.Summary, "parameters": params,
			"security": []any{map[string]any{"bearerAuth": []any{}}},
			"responses": map[string]any{
				"200": map[string]any{"description": op.Response, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object"}}}},
				"401": map[string]any{"description": "Missing, invalid, expired or revoked token."},
				"404": map[string]any{"description": "Not found (also returned for sensitive items)."},
				"429": map[string]any{"description": "Too many failed attempts or requests; honour Retry-After."},
			},
		}
		paths[op.Path] = map[string]any{"get": get}
	}
	doc := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "Daka read-only API",
			"version":     "1.0.0",
			"description": "Read-only access for AI agents. Authenticate with a read-scope API token created in the web UI. No endpoint here can modify data. Sensitive items are always excluded.",
		},
		"servers":    []any{map[string]any{"url": "/"}},
		"paths":      paths,
		"components": map[string]any{"securitySchemes": map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer", "description": "Authorization: Bearer dk_... (read scope)"}}},
	}
	b, _ := json.Marshal(doc)
	return b
}

// GET /api/openapi.json: static, unauthenticated, secret-free.
func (s *server) getOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		errResult(http.StatusMethodNotAllowed, "method not allowed").write(w)
		return
	}
	openAPIOnce.Do(func() { openAPIBody = buildOpenAPI() })
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(openAPIBody)
}
