package markdown

import (
	"testing"

	"charm.land/glamour/v2/styles"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func TestGetMarkdownRendererNilContext(t *testing.T) {
	markdownStyle = nil
	markdownStyleSource = ""

	require.NotPanics(t, func() {
		renderer := GetMarkdownRenderer(80, nil)
		rendered, err := renderer.Render("# hello")
		require.NoError(t, err)
		require.NotEmpty(t, rendered)
	})
}

func TestInitializeMarkdownStyleNilContext(t *testing.T) {
	markdownStyle = nil
	markdownStyleSource = ""

	require.NotPanics(t, func() {
		InitializeMarkdownStyle(nil)
	})

	require.NotNil(t, markdownStyle)
}

func TestRendererCachesByWidthAndStyle(t *testing.T) {
	ctx := &context.ProgramContext{HasDarkBackground: true}
	body := "Some **bold** text that is long enough to wrap at a narrow width"

	narrow := GetMarkdownRenderer(20, ctx)
	first, err := narrow.Render(body)
	if err != nil {
		t.Fatal(err)
	}
	second, err := narrow.Render(body)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("rendering the same markdown twice gave different results")
	}

	wide, err := GetMarkdownRenderer(80, ctx).Render(body)
	if err != nil {
		t.Fatal(err)
	}
	if wide == first {
		t.Fatal("different widths shared a cache entry")
	}

	light := Renderer{width: 20, style: &styles.LightStyleConfig}
	lightRendered, err := light.Render(body)
	if err != nil {
		t.Fatal(err)
	}
	if lightRendered == first {
		t.Fatal("different styles shared a cache entry")
	}
}
