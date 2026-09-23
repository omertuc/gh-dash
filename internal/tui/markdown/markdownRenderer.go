package markdown

import (
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	log "charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

var (
	markdownStyle       *ansi.StyleConfig
	markdownStyleSource string
)

func InitializeMarkdownStyle(ctx *context.ProgramContext) {
	// Don't let a less reliable source override what the terminal reported.
	if markdownStyle != nil && markdownStyleSource == "bubbletea" &&
		(ctx == nil || ctx.BackgroundSource != "bubbletea") {
		log.Debugf("InitializeMarkdownStyle: keeping existing bubbletea style")
		return
	}

	hasDarkBackground := true
	backgroundSource := "default"
	if ctx != nil {
		hasDarkBackground = ctx.HasDarkBackground
		backgroundSource = ctx.BackgroundSource
	}

	if hasDarkBackground {
		markdownStyle = &CustomDarkStyleConfig
	} else {
		markdownStyle = &styles.LightStyleConfig
	}
	markdownStyleSource = backgroundSource

	log.Debugf(
		"InitializeMarkdownStyle: assigned ctx.hasDarkBackground=%t, markdownStyleSource=%q",
		hasDarkBackground,
		markdownStyleSource,
	)
}

// Renderer renders markdown at a fixed word-wrap width. Results are cached,
// since views re-render the same comments on every redraw and glamour
// rendering is expensive.
type Renderer struct {
	width int
	style *ansi.StyleConfig
}

type renderCacheKey struct {
	style    *ansi.StyleConfig
	width    int
	markdown string
}

type termRendererKey struct {
	style *ansi.StyleConfig
	width int
}

// maxRenderCacheEntries bounds the render cache. When it fills up the cache
// is simply cleared; it refills with whatever is currently on screen.
const maxRenderCacheEntries = 1000

var (
	renderMu      sync.Mutex
	renderCache   = map[renderCacheKey]string{}
	termRenderers = map[termRendererKey]*glamour.TermRenderer{}
)

func GetMarkdownRenderer(width int, ctx *context.ProgramContext) Renderer {
	if markdownStyle == nil {
		InitializeMarkdownStyle(ctx)
	}
	return Renderer{width: width, style: markdownStyle}
}

func (r Renderer) Render(markdown string) (string, error) {
	renderMu.Lock()
	defer renderMu.Unlock()

	cacheKey := renderCacheKey{style: r.style, width: r.width, markdown: markdown}
	if rendered, ok := renderCache[cacheKey]; ok {
		return rendered, nil
	}

	rendered, err := r.termRenderer().Render(markdown)
	if err != nil {
		return rendered, err
	}

	if len(renderCache) >= maxRenderCacheEntries {
		clear(renderCache)
	}
	renderCache[cacheKey] = rendered
	return rendered, nil
}

// termRenderer returns a glamour renderer for r's style and width, reusing
// one when possible since creating them is expensive. renderMu must be held.
func (r Renderer) termRenderer() *glamour.TermRenderer {
	key := termRendererKey{style: r.style, width: r.width}
	if tr, ok := termRenderers[key]; ok {
		return tr
	}

	tr, err := glamour.NewTermRenderer(
		glamour.WithStyles(*r.style),
		glamour.WithWordWrap(r.width),
	)
	if err != nil || tr == nil {
		// Fall back to a renderer that just returns input unchanged
		tr, _ = glamour.NewTermRenderer()
		if tr == nil {
			// If even fallback fails, panic with helpful message
			msg := "unknown error"
			if err != nil {
				msg = err.Error()
			}
			panic("failed to create markdown renderer: " + msg)
		}
	}

	termRenderers[key] = tr
	return tr
}
