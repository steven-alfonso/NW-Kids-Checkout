// Package menu renders the shared kebab menu links server-side so that
// auth-gated destinations (e.g. /admin) are never shipped to anonymous
// clients in static JS or HTML.
package menu

import (
	"bytes"
	"html/template"
)

// Placeholder is the marker inside static page HTML where the rendered menu
// links are injected.
const Placeholder = "<!-- kebab-menu-links -->"

// Item is a single kebab menu link.
type Item struct {
	ID    string
	Label string
	Href  string
}

// For returns the kebab menu items visible to the given session. Guest
// check-in and manual check-ins are always visible. Log In is only shown to
// anonymous users; Log Out only to authenticated users; Admin only to admin
// users.
func For(authenticated bool, role string) []Item {
	items := []Item{
		{ID: "guest-checkin-link", Label: "Guest Check-In", Href: "/guest-checkin"},
		{ID: "manual-checkins-link", Label: "Manual Check-Ins", Href: "/manual-checkins"},
	}
	if !authenticated {
		items = append(items, Item{ID: "login-link", Label: "Log In", Href: "/login?next=/"})
		return items
	}
	if role == "admin" {
		items = append(items, Item{ID: "admin-link", Label: "Admin", Href: "/admin"})
	}
	items = append(items, Item{ID: "logout-link", Label: "Log Out", Href: "/logout"})
	return items
}

const itemClass = "flex items-center justify-between px-4 py-2 font-semibold transition hover:bg-slate-50 hover:text-slate-900"

type menuData struct {
	Items []Item
	Class string
}

var menuTmpl = template.Must(template.New("kebab-menu").Parse(
	`{{- range .Items -}}<a id="{{.ID}}" href="{{.Href}}" class="{{$.Class}}">{{.Label}}<span aria-hidden="true">→</span></a>{{- end -}}`,
))

// RenderHTML returns the kebab menu link markup for the given session.
// Labels, ids, and hrefs are template-escaped, so the output is safe even if
// item values ever become dynamic.
func RenderHTML(authenticated bool, role string) (string, error) {
	var buf bytes.Buffer
	if err := menuTmpl.Execute(&buf, menuData{
		Items: For(authenticated, role),
		Class: itemClass,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
