package yamldiff

// These let the external tests that read inputs through the load package
// share the golden helpers.
var (
	CompareCases       = compareCases
	CheckCompareGolden = checkCompareGolden
)
