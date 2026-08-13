package operators

// Helpers connecting the parsed ":nocache" modifier to backend cache
// layers. Opcall.Run publishes the running call's flag as ev.NoCache
// (ambient per-call context, set and restored exactly like ev.Target), so
// a caching operator needs no Operator interface change to honor it.

// ShouldSkipCache reports whether the operator call currently running was
// written with the ":nocache" modifier (e.g. "(( vault:nocache ... ))").
// Caching operators call this before both the cache read AND the cache
// write: a nocache fetch must neither be served from nor refresh the
// shared cache entry.
func ShouldSkipCache(ev *Evaluator) bool {
	return ev != nil && ev.NoCache
}

// WithNoCacheCheck marks (or unmarks) a response's NoCache flag so cache
// layers between the operator and the document can decline to store it.
// It mutates and returns the same response.
func WithNoCacheCheck(result *Response, skipCache bool) *Response {
	if result != nil {
		result.NoCache = skipCache
	}
	return result
}

// IsNoCacheResponse reports whether a response was marked as
// not-to-be-cached. Safe on a nil response.
func IsNoCacheResponse(result *Response) bool {
	return result != nil && result.NoCache
}
