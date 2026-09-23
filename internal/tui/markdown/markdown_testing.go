package markdown

// RenderCacheSizeForTesting returns how many rendered documents are cached,
// e.g. to check that content was rendered ahead of time.
func RenderCacheSizeForTesting() int {
	renderMu.Lock()
	defer renderMu.Unlock()
	return len(renderCache)
}
