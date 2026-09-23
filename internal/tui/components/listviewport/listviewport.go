package listviewport

import (
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type Model struct {
	ctx             context.ProgramContext
	viewport        viewport.Model
	topBoundId      int
	bottomBoundId   int
	currId          int
	ListItemHeight  int
	NumCurrentItems int
	NumTotalItems   int
	LastUpdated     time.Time
	CreatedAt       time.Time
	ItemTypeLabel   string

	// contentGen identifies the content last passed to SyncViewPort. It comes
	// from a global counter so that no two copies of a Model ever share a
	// generation for different content.
	contentGen uint64
	viewCache  *viewCache
}

var contentGenCounter atomic.Uint64

type viewCacheKey struct {
	contentGen    uint64
	yOffset       int
	xOffset       int
	width, height int
}

// viewCache memoizes View, which is called on every frame but whose inputs
// only change when the content, scroll position or size change.
type viewCache struct {
	key  viewCacheKey
	view string
}

func NewModel(
	ctx context.ProgramContext,
	dimensions constants.Dimensions,
	lastUpdated time.Time,
	createdAt time.Time,
	itemTypeLabel string,
	numItems, listItemHeight int,
) Model {
	model := Model{
		ctx:             ctx,
		NumCurrentItems: numItems,
		ListItemHeight:  listItemHeight,
		currId:          0,
		viewport: viewport.New(
			viewport.WithWidth(dimensions.Width),
			viewport.WithHeight(dimensions.Height),
		),
		topBoundId:    0,
		ItemTypeLabel: itemTypeLabel,
		LastUpdated:   lastUpdated,
		CreatedAt:     createdAt,
		viewCache:     &viewCache{},
	}
	model.bottomBoundId = utils.Min(
		model.NumCurrentItems-1,
		model.getNumPrsPerPage()-1,
	)
	return model
}

func (m *Model) SetNumItems(numItems int) {
	m.NumCurrentItems = numItems
	// Keep the bounds of what's scrolled to, rather than resetting them to
	// the top while the view stays scrolled
	m.bottomBoundId = utils.Min(m.NumCurrentItems-1, m.topBoundId+m.getNumPrsPerPage()-1)
}

// ReplaceItems sets the number of items after they were replaced, and moves
// to the given item, keeping it where the current item is shown in the view.
// This way the selection doesn't jump around when items are added or removed
// above it. The view scrolls there once the new items' content is synced.
func (m *Model) ReplaceItems(numItems, item int) int {
	perPage := m.getNumPrsPerPage()
	offset := utils.Max(0, m.currId-m.topBoundId)
	if perPage > 0 {
		offset = utils.Min(offset, perPage-1)
	}

	m.NumCurrentItems = numItems
	item = utils.Max(0, utils.Min(item, numItems-1))
	top := utils.Max(0, item-offset)
	if perPage > 0 {
		// Don't leave empty space below the last item
		top = utils.Max(0, utils.Min(top, numItems-perPage))
	}

	m.currId = item
	m.topBoundId = top
	m.bottomBoundId = utils.Min(numItems-1, top+perPage-1)
	return m.currId
}

// ScrollToTopBound scrolls the view to the first item it should show.
func (m *Model) ScrollToTopBound() {
	m.viewport.SetYOffset(m.topBoundId * m.ListItemHeight)
}

func (m *Model) SetTotalItems(total int) {
	m.NumTotalItems = total
}

func (m *Model) SetItemHeight(height int) {
	m.ListItemHeight = height
}

func (m *Model) SyncViewPort(content string) {
	m.viewport.SetContent(content)
	m.contentGen = contentGenCounter.Add(1)
}

func (m *Model) getNumPrsPerPage() int {
	if m.ListItemHeight == 0 {
		return 0
	}
	return m.viewport.Height() / m.ListItemHeight
}

func (m *Model) ResetCurrItem() {
	m.FirstItem()
}

func (m *Model) GetCurrItem() int {
	return m.currId
}

func (m *Model) NextItem() int {
	atBottomOfViewport := m.currId >= m.bottomBoundId
	if atBottomOfViewport {
		m.topBoundId += 1
		m.bottomBoundId += 1
		m.viewport.ScrollDown(m.ListItemHeight)
	}

	newId := utils.Min(m.currId+1, m.NumCurrentItems-1)
	newId = utils.Max(newId, 0)
	m.currId = newId
	return m.currId
}

func (m *Model) PrevItem() int {
	if m.currId > 0 && m.currId <= m.topBoundId {
		m.topBoundId -= 1
		m.bottomBoundId -= 1
		m.viewport.ScrollUp(m.ListItemHeight)
	}

	m.currId = utils.Max(m.currId-1, 0)
	return m.currId
}

// ItemAtLine returns the item shown on the given line of the view, or -1 when
// there's none there.
func (m *Model) ItemAtLine(line int) int {
	if m.ListItemHeight <= 0 || line < 0 || line >= m.viewport.Height() {
		return -1
	}
	item := (line + m.viewport.YOffset()) / m.ListItemHeight
	if item >= m.NumCurrentItems {
		return -1
	}
	return item
}

// SelectItem moves to the given item, scrolling the same way moving there one
// item at a time would.
func (m *Model) SelectItem(item int) int {
	item = utils.Min(item, m.NumCurrentItems-1)
	for m.currId < item {
		m.NextItem()
	}
	for m.currId > item && item >= 0 {
		m.PrevItem()
	}
	return m.currId
}

func (m *Model) FirstItem() int {
	m.currId = 0
	m.topBoundId = 0
	m.bottomBoundId = utils.Min(m.NumCurrentItems-1, m.getNumPrsPerPage()-1)
	m.viewport.GotoTop()
	return m.currId
}

func (m *Model) LastItem() int {
	m.currId = m.NumCurrentItems - 1
	m.topBoundId = utils.Max(0, m.NumCurrentItems-m.getNumPrsPerPage())
	m.bottomBoundId = m.NumCurrentItems - 1
	m.viewport.GotoBottom()
	return m.currId
}

func (m *Model) SetDimensions(dimensions constants.Dimensions) {
	m.viewport.SetHeight(max(0, dimensions.Height))
	m.viewport.SetWidth(max(0, dimensions.Width))
}

func (m *Model) View() string {
	key := viewCacheKey{
		contentGen: m.contentGen,
		yOffset:    m.viewport.YOffset(),
		xOffset:    m.viewport.XOffset(),
		width:      m.viewport.Width(),
		height:     m.viewport.Height(),
	}
	if m.viewCache != nil && m.contentGen != 0 && m.viewCache.key == key {
		return m.viewCache.view
	}

	viewport := m.viewport.View()
	view := lipgloss.NewStyle().
		Width(m.viewport.Width()).
		MaxWidth(m.viewport.Width()).
		Render(
			viewport,
		)
	if m.viewCache == nil {
		m.viewCache = &viewCache{}
	}
	m.viewCache.key = key
	m.viewCache.view = view
	return view
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = *ctx
}
