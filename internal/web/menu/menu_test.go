package menu

import (
	"io"
	"strings"
	"testing"

	"kids-checkin/internal/web/static"

	"github.com/stretchr/testify/require"
)

func TestFor(t *testing.T) {
	tests := []struct {
		name          string
		authenticated bool
		role          string
		want          []string
	}{
		{
			name: "anonymous user",
			want: []string{"manual-checkins-link", "login-link"},
		},
		{
			name:          "authenticated non-admin user",
			authenticated: true,
			role:          "user",
			want:          []string{"manual-checkins-link", "logout-link"},
		},
		{
			name:          "authenticated admin user",
			authenticated: true,
			role:          "admin",
			want:          []string{"manual-checkins-link", "admin-link", "logout-link"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := For(tt.authenticated, tt.role)
			ids := make([]string, len(items))
			for i, item := range items {
				ids[i] = item.ID
			}
			require.Equal(t, tt.want, ids)
		})
	}
}

func TestRenderHTML(t *testing.T) {
	html, err := RenderHTML(true, "admin")
	require.NoError(t, err)
	require.Contains(t, html, `id="manual-checkins-link"`)
	require.Contains(t, html, `href="/admin"`)
	require.Contains(t, html, `href="/logout"`)
	require.NotContains(t, html, `href="/login`)

	html, err = RenderHTML(false, "")
	require.NoError(t, err)
	require.Contains(t, html, `href="/login?next=/"`)
	require.NotContains(t, html, `href="/admin"`)
	require.NotContains(t, html, `href="/logout"`)
}

func TestPageHTMLUsesMenuPlaceholder(t *testing.T) {
	paths := []string{
		"pages/home/index.html",
		"pages/checkoutsv1/checkouts.html",
		"pages/manual-checkins/index.html",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			f, err := static.EmbeddedFS.Open(path)
			require.NoError(t, err)
			defer f.Close()

			content, err := io.ReadAll(f)
			require.NoError(t, err)
			require.Contains(t, string(content), Placeholder, "page must use the menu placeholder so links are rendered server-side")
			require.NotContains(t, string(content), "/admin", "page must not hardcode auth-gated routes")
		})
	}
}

func TestRenderedMenuFitsPlaceholder(t *testing.T) {
	for _, role := range []string{"", "user", "admin"} {
		html, err := RenderHTML(role != "", role)
		require.NoError(t, err)
		require.False(t, strings.Contains(html, "class=\"hidden"), "server-rendered menu should not need hidden classes")
		require.True(t, strings.HasPrefix(html, `<a id="manual-checkins-link"`), "manual check-ins link must always be first")
	}
}
