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

// prerenderMu serializes prerendering, which uses its own glamour renderers
// so it doesn't hold renderMu, and so doesn't hold up rendering what's on
// screen, while rendering.
var (
	prerenderMu            sync.Mutex
	prerenderTermRenderers = map[termRendererKey]*glamour.TermRenderer{}
)

// Prerender renders the given markdown documents into the cache, so that
// rendering them later is instant. It's meant to be run in the background,
// e.g. for content that is likely to be shown next.
func (r Renderer) Prerender(documents ...string) {
	prerenderMu.Lock()
	defer prerenderMu.Unlock()

	for _, markdown := range documents {
		cacheKey := renderCacheKey{style: r.style, width: r.width, markdown: markdown}
		renderMu.Lock()
		_, ok := renderCache[cacheKey]
		renderMu.Unlock()
		if ok {
			continue
		}

		key := termRendererKey{style: r.style, width: r.width}
		tr, ok := prerenderTermRenderers[key]
		if !ok {
			tr = newTermRenderer(r.style, r.width)
			prerenderTermRenderers[key] = tr
		}
		rendered, err := tr.Render(markdown)
		if err != nil {
			continue
		}

		renderMu.Lock()
		if len(renderCache) >= maxRenderCacheEntries {
			clear(renderCache)
		}
		renderCache[cacheKey] = rendered
		renderMu.Unlock()
	}
}

// termRenderer returns a glamour renderer for r's style and width, reusing
// one when possible since creating them is expensive. renderMu must be held.
func (r Renderer) termRenderer() *glamour.TermRenderer {
	key := termRendererKey{style: r.style, width: r.width}
	if tr, ok := termRenderers[key]; ok {
		return tr
	}

	tr := newTermRenderer(r.style, r.width)
	termRenderers[key] = tr
	return tr
}

func newTermRenderer(style *ansi.StyleConfig, width int) *glamour.TermRenderer {
	tr, err := glamour.NewTermRenderer(
		glamour.WithStyles(*style),
		glamour.WithWordWrap(width),
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
	return tr
}
