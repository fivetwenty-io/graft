package graft

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/cppforlife/go-patch/patch"

	"github.com/fivetwenty-io/graft/pkg/graft/interfaces"
	"github.com/fivetwenty-io/graft/pkg/graft/merger"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// mergeBuilderImpl implements the MergeBuilder interface.
type mergeBuilderImpl struct {
	engine         Engine
	ctx            context.Context
	docs           []Document
	pruneKeys      []string
	cherryPickKeys []string
	skipEvaluation bool
	goPatch        bool
	fallbackAppend bool
	arrayStrategy  ArrayMergeStrategy
	error          error                 // Stores any error from construction
	mergeMetadata  *merger.MergeMetadata // Accumulated metadata from merges

	// priorCalcValues records, per canonical path string, the base value a
	// "(( calc <leading-op> ... ))" value-modification expression
	// overwrote during merge. Populated by
	// mergeValuesAtPath; consumed by applyEvaluation via
	// WithPriorCalcValues. nil for the overwhelming majority of merges that
	// never write this expression shape — no cost for any other document.
	priorCalcValues map[string]interface{}
}

// WithPrune adds keys to remove from the final output.
func (m *mergeBuilderImpl) WithPrune(keys ...string) MergeBuilder {
	if m.error != nil {
		return m // Propagate error
	}

	newBuilder := *m // Copy the builder
	newBuilder.pruneKeys = append(m.pruneKeys, keys...)
	return &newBuilder
}

// WithCherryPick specifies keys to keep in the final output.
func (m *mergeBuilderImpl) WithCherryPick(keys ...string) MergeBuilder {
	if m.error != nil {
		return m // Propagate error
	}

	newBuilder := *m // Copy the builder
	newBuilder.cherryPickKeys = append(m.cherryPickKeys, keys...)
	return &newBuilder
}

// SkipEvaluation disables operator evaluation after merging.
func (m *mergeBuilderImpl) SkipEvaluation() MergeBuilder {
	if m.error != nil {
		return m // Propagate error
	}

	newBuilder := *m // Copy the builder
	newBuilder.skipEvaluation = true
	return &newBuilder
}

// EnableGoPatch enables go-patch format parsing.
func (m *mergeBuilderImpl) EnableGoPatch() MergeBuilder {
	if m.error != nil {
		return m // Propagate error
	}

	newBuilder := *m // Copy the builder
	newBuilder.goPatch = true
	return &newBuilder
}

// FallbackAppend uses append instead of inline for arrays by default.
func (m *mergeBuilderImpl) FallbackAppend() MergeBuilder {
	if m.error != nil {
		return m // Propagate error
	}

	newBuilder := *m // Copy the builder
	newBuilder.fallbackAppend = true
	newBuilder.arrayStrategy = AppendArrays
	return &newBuilder
}

// WithArrayMergeStrategy sets how arrays are merged.
func (m *mergeBuilderImpl) WithArrayMergeStrategy(strategy ArrayMergeStrategy) MergeBuilder {
	if m.error != nil {
		return m // Propagate error
	}

	newBuilder := *m // Copy the builder
	newBuilder.arrayStrategy = strategy
	// Update fallbackAppend based on strategy
	if strategy == AppendArrays {
		newBuilder.fallbackAppend = true
	}
	return &newBuilder
}

// Execute performs the merge operation.
//
//nolint:gocyclo // merge execution handles many edge cases and options
func (m *mergeBuilderImpl) Execute() (Document, error) {
	// Check for construction errors first
	if m.error != nil {
		return nil, m.error
	}

	// Check context cancellation
	select {
	case <-m.ctx.Done():
		return nil, m.ctx.Err()
	default:
	}

	// Handle empty document list
	if len(m.docs) == 0 {
		return NewDocument(make(map[string]interface{})), nil
	}

	// Reject a cyclic document before any of the recursive tree walks below
	// run: hasArrayOperators/hasArraysWithMaps/hasPruneOperators/
	// hasSortOperators (called on every document just past this point, for
	// both the single- and multi-document paths) and deepCopyMap/
	// deepCopyValue (called during the merge itself) all recurse over
	// map[string]interface{}/[]interface{} without their own visited
	// tracking cost paid on every call; a single up-front walk here catches
	// a self-referencing document — buildable through the library API via
	// NewDocument(map[string]interface{}) with no validation of its own —
	// before any of them can overflow the stack on it.
	for i, doc := range m.docs {
		if IsGoPatchDocument(doc) {
			continue // go-patch documents carry patch.Ops, not raw map data
		}
		if data, ok := doc.RawData().(map[string]interface{}); ok {
			if err := detectCyclicValue(data); err != nil {
				return nil, fmt.Errorf("document %d: %w", i, err)
			}
		}
	}

	// Handle single document case. A lone go-patch document (no base to
	// patch) falls through to mergeDocuments below, which patches an empty
	// map at that document's position, matching spruce's behavior for a
	// single-file go-patch invocation.
	if len(m.docs) == 1 && !IsGoPatchDocument(m.docs[0]) {
		// For single documents, we need to validate arrays even without merging
		// to match legacy behavior for Issue #172
		data, ok := m.docs[0].RawData().(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("document data is not a map")
		}

		// Check if we need to use the merger:
		// 1. If there are array-merge markers (append/prepend/merge/insert/delete/...)
		//    These are merge-phase constructs and must always be resolved, even
		//    under --skip-eval (which only skips the evaluator phase).
		// 2. If there are arrays with maps (for default key-merge/validation) -
		//    also a merge-phase concern, independent of --skip-eval.
		// 3. If there are prune operators AND we're not skipping evaluation -
		//    (( prune )) on a lone, never-overwritten value is resolved by the
		//    evaluator, so it must stay literal when evaluation is skipped.
		useArrayOperators := m.hasArrayOperators(data)
		hasArraysWithMaps := m.hasArraysWithMaps(data)
		hasPruneOps := m.hasPruneOperators(data) && !m.skipEvaluation

		if useArrayOperators || hasArraysWithMaps || hasPruneOps {
			// Process through merger for validation and/or array operators
			mergerInstance := &merger.Merger{
				AppendByDefault: m.fallbackAppend,
			}

			// Set memory tracker if available from the engine
			if defaultEngine, ok := m.engine.(*DefaultEngine); ok && defaultEngine.documentMemory != nil {
				mergerInstance.SetMemoryTracker(defaultEngine.documentMemory)
			}

			// Create an empty base and merge our document into it
			// This triggers the array validation logic
			base := make(map[string]interface{})
			err := mergerInstance.Merge(base, data)
			if err != nil {
				// Convert merger.MultiError to graft.MultiError for consistent error formatting
				var mergerMultiErr merger.MultiError
				if errors.As(err, &mergerMultiErr) {
					graftMultiErr := &MultiError{}
					for _, e := range mergerMultiErr.Errors {
						graftMultiErr.Append(e)
					}
					return nil, graftMultiErr
				}
				return nil, err
			}

			return m.applyPostProcessing(NewDocument(base))
		}

		// No special processing needed, just clone
		result := m.docs[0].Clone()
		return m.applyPostProcessing(result)
	}

	// Merge multiple documents
	result, err := m.mergeDocuments()
	if err != nil {
		return nil, err
	}

	return m.applyPostProcessing(result)
}

// mergeDocuments performs the actual document merging.
//
// Documents are processed in file order, exactly matching spruce's own merge
// loop: a regular (YAML) document merges onto the running result, and a
// go-patch document replaces the running result with ops.Apply(result) at
// the position it appears. A patch positioned between two regular overlays
// therefore only affects documents merged before it — later overlays keep
// merging on top of the patched result, instead of every patch being hoisted
// to the end of the whole file sequence.
//
//nolint:gocyclo // document merging has complex logic for go-patch and prune operators
func (m *mergeBuilderImpl) mergeDocuments() (Document, error) {
	var result map[string]interface{}
	haveResult := false

	for _, doc := range m.docs {
		// Check context cancellation during merge
		select {
		case <-m.ctx.Done():
			return nil, m.ctx.Err()
		default:
		}

		if IsGoPatchDocument(doc) {
			ops, ok := GetGoPatchOps(doc)
			if !ok {
				continue
			}
			if !haveResult {
				// A leading go-patch document (no base merged yet) patches an
				// empty map, matching spruce's `root := make(map[...]...)`
				// starting point.
				result = make(map[string]interface{})
				haveResult = true
			}
			patched, err := applyGoPatchToMap(result, ops)
			if err != nil {
				return nil, err
			}
			result = patched
			continue
		}

		overlayData, ok := doc.RawData().(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("document data is not a map")
		}

		if !haveResult {
			baseResult, err := m.prepareFirstDocument(overlayData)
			if err != nil {
				return nil, err
			}
			result = baseResult
			haveResult = true
			continue
		}

		if err := m.mergeInto(result, overlayData); err != nil {
			// Check if this is a detailed merger error that should be preserved
			if isMergerError(err) {
				return nil, err
			}
			return nil, NewMergeError("failed to merge documents", err)
		}
	}

	if !haveResult {
		return NewDocument(make(map[string]interface{})), nil
	}

	return NewDocument(result), nil
}

// prepareFirstDocument resolves array-merge markers, arrays-with-maps
// default key-merge validation, and (( prune )) ghost-tracking on the first
// document that contributes to the merge result. This mirrors the
// merge-phase processing mergeInto applies to every later overlay, run once
// against an empty base since there is nothing yet to merge onto — the same
// treatment as spruce's initial `m.Merge(root, doc)` call against an empty
// root. Array-merge markers and arrays-with-maps are merge-phase constructs
// and must be resolved regardless of --skip-eval; prune operators are only
// ghost-tracked here when evaluation will run (a never-overwritten
// (( prune )) on a lone document is resolved by the evaluator, not by
// merge-phase ghosting).
func (m *mergeBuilderImpl) prepareFirstDocument(baseData map[string]interface{}) (map[string]interface{}, error) {
	needsProcessing := m.hasArrayOperators(baseData) ||
		m.hasArraysWithMaps(baseData) ||
		(m.hasPruneOperators(baseData) && !m.skipEvaluation)
	if !needsProcessing {
		return deepCopyMap(baseData)
	}

	mergerInstance := &merger.Merger{
		AppendByDefault: m.fallbackAppend,
	}
	if m.engine != nil {
		if tracker := m.engine.GetMemoryTracker(); tracker != nil {
			mergerInstance.SetMemoryTracker(tracker)
		}
	}

	emptyBase := make(map[string]interface{})
	if err := mergerInstance.Merge(emptyBase, baseData); err != nil {
		return nil, m.convertMergerError(err)
	}

	m.collectMergeMetadata(mergerInstance)

	return emptyBase, nil
}

// needsLegacyMerger determines if the legacy merger should be used.
//
// Array-merge markers, arrays-with-maps default key-merge validation, and
// (( sort by X )) ghost-tracking are all merge-phase constructs that must be
// resolved identically whether or not --skip-eval is set (skip-eval only
// disables the evaluator phase, not the merger). Prune ghost-tracking is
// intentionally left unconditional here too: once a (( prune )) marker has
// been overwritten by a later document it must stay queued for removal
// regardless of --skip-eval, matching spruce's merge.go behavior.
func (m *mergeBuilderImpl) needsLegacyMerger(base, overlay map[string]interface{}) bool {
	return m.hasArrayOperators(overlay) ||
		m.hasArraysWithMaps(overlay) ||
		m.hasPruneOperators(overlay) ||
		m.hasPruneOperators(base) ||
		m.hasSortOperators(overlay) ||
		m.hasSortOperators(base)
}

// performLegacyMerge uses the legacy merger for complex merge operations.
func (m *mergeBuilderImpl) performLegacyMerge(base, overlay map[string]interface{}) error {
	mergerInstance := &merger.Merger{
		AppendByDefault: m.fallbackAppend,
	}

	if m.engine != nil {
		if tracker := m.engine.GetMemoryTracker(); tracker != nil {
			mergerInstance.SetMemoryTracker(tracker)
		}
	}

	baseCopy, err := deepCopyMap(base)
	if err != nil {
		return err
	}

	err = mergerInstance.Merge(baseCopy, overlay)
	if err != nil {
		return m.convertMergerError(err)
	}

	m.collectMergeMetadata(mergerInstance)

	for key, value := range baseCopy {
		base[key] = value
	}
	return nil
}

// convertMergerError converts merger errors to graft errors.
func (m *mergeBuilderImpl) convertMergerError(err error) error {
	var mergerMultiErr merger.MultiError
	if errors.As(err, &mergerMultiErr) {
		graftMultiErr := &MultiError{}
		for _, e := range mergerMultiErr.Errors {
			graftMultiErr.Append(e)
		}
		return graftMultiErr
	}
	return err
}

// collectMergeMetadata collects metadata from a merge operation.
func (m *mergeBuilderImpl) collectMergeMetadata(mergerInstance *merger.Merger) {
	metadata := mergerInstance.GetMetadata()
	if metadata == nil || (len(metadata.PrunePaths) == 0 && len(metadata.SortPaths) == 0) {
		return
	}

	if m.mergeMetadata == nil {
		m.mergeMetadata = &merger.MergeMetadata{
			SortPaths: make(map[string]string),
		}
	}
	m.mergeMetadata.PrunePaths = append(m.mergeMetadata.PrunePaths, metadata.PrunePaths...)
	for k, v := range metadata.SortPaths {
		m.mergeMetadata.SortPaths[k] = v
	}
}

// mergeInto merges overlay data into base data using legacy merger when needed.
func (m *mergeBuilderImpl) mergeInto(base, overlay map[string]interface{}) error {
	return m.mergeIntoAtPath(base, overlay, nil)
}

// mergeIntoAtPath is mergeInto with the canonical path (as a segment slice,
// root-relative) of `base`/`overlay` within the overall document. The path
// is threaded through purely to let mergeValuesAtPath record calc
// prior-values at the correct key; it changes no
// merge decision.
func (m *mergeBuilderImpl) mergeIntoAtPath(base, overlay map[string]interface{}, path []string) error {
	if m.needsLegacyMerger(base, overlay) {
		m.recordPriorCalcValuesUnder(base, overlay, path)
		return m.performLegacyMerge(base, overlay)
	}

	return m.performSimpleMergeAtPath(base, overlay, path)
}

// recordPriorCalcValuesUnder records the calc value-modification prior
// values for a subtree the legacy merger (pkg/graft/merger) is about to
// merge. That merger has no recording hook of its own, and it handles every
// document containing an array-merge marker, an array of maps, or a prune /
// sort marker — the shape most real manifests take — so without this pass
// the leading-operator form would silently evaluate against nothing and
// yield 0 for exactly those documents. It reads both maps and writes only
// m.priorCalcValues, so it changes no merge decision.
//
// Values inside lists are deliberately not recorded: their post-merge
// indices depend on merge decisions (append vs. merge-by-key) this pass runs
// before, so no key it could write would be certain to match ev.Here.
func (m *mergeBuilderImpl) recordPriorCalcValuesUnder(base, overlay map[string]interface{}, path []string) {
	for key, overlayValue := range overlay {
		baseValue, inBase := base[key]
		if !inBase {
			continue
		}

		childPath := make([]string, len(path)+1)
		copy(childPath, path)
		childPath[len(path)] = key

		switch ov := overlayValue.(type) {
		case map[string]interface{}:
			if bv, ok := baseValue.(map[string]interface{}); ok {
				m.recordPriorCalcValuesUnder(bv, ov, childPath)
			}
		case string:
			if isCalcModificationExpression(ov) {
				m.recordPriorCalcValue(childPath, baseValue)
			}
		}
	}
}

// performSimpleMerge performs a simple merge without legacy merger.
func (m *mergeBuilderImpl) performSimpleMerge(base, overlay map[string]interface{}) error {
	return m.performSimpleMergeAtPath(base, overlay, nil)
}

// performSimpleMergeAtPath is performSimpleMerge with the path context
// mergeIntoAtPath threads through; see its doc comment.
//
// path is the canonical no-"$" dotted history vocabulary (P0-2, matching
// pkg/graft/tree.Cursor.String()) of base/overlay's own location, one
// segment per ancestor map key, root-relative (nil at the top level).
// Recording calls join path with each iteration's own key rather than
// recording the bare key alone: this function recurses into nested maps via
// mergeValuesAtPath/mergeIntoAtPath, so without the prefix a change three
// levels deep would be recorded under its bare leaf name (e.g. "host"),
// indistinguishable from an unrelated sibling subtree's own "host" key, and
// would never match the same path's eval-phase entry (which always carries
// its full dotted location).
func (m *mergeBuilderImpl) performSimpleMergeAtPath(base, overlay map[string]interface{}, path []string) error {
	var memTracker interfaces.MemoryTracker
	if m.engine != nil {
		if tracker := m.engine.GetMemoryTracker(); tracker != nil && tracker.IsEnabled() {
			memTracker = tracker
		}
	}
	pathPrefix := strings.Join(path, ".")

	for key, overlayValue := range overlay {
		baseValue, exists := base[key]
		keyStr := fmt.Sprintf("%v", key)
		recordPath := keyStr
		if pathPrefix != "" {
			recordPath = pathPrefix + "." + keyStr
		}

		if !exists {
			copied, err := deepCopyValue(overlayValue)
			if err != nil {
				return err
			}
			base[key] = copied
			if memTracker != nil {
				_ = memTracker.RecordMergeChange(recordPath, nil, overlayValue, "add")
			}
			continue
		}

		// A fresh copy per key: path is shared across sibling iterations of
		// this loop, and append may otherwise reuse (and silently
		// overwrite) the same backing array across them.
		childPath := make([]string, len(path)+1)
		copy(childPath, path)
		childPath[len(path)] = keyStr

		merged, err := m.mergeValuesAtPath(baseValue, overlayValue, childPath)
		if err != nil {
			return err
		}
		if merged == nil {
			delete(base, key)
			if memTracker != nil {
				_ = memTracker.RecordMergeChange(recordPath, baseValue, nil, "delete")
			}
		} else {
			base[key] = merged
			// Record the merge if value changed
			if memTracker != nil && !valuesEqual(baseValue, merged) {
				_ = memTracker.RecordMergeChange(recordPath, baseValue, merged, "merge")
			}
		}
	}

	return nil
}

// mergeValues merges two values based on their types.
func (m *mergeBuilderImpl) mergeValues(base, overlay interface{}) (interface{}, error) {
	return m.mergeValuesAtPath(base, overlay, nil)
}

// mergeValuesAtPath is mergeValues with the canonical path of base/overlay
// within the overall document; see mergeIntoAtPath's doc comment.
func (m *mergeBuilderImpl) mergeValuesAtPath(base, overlay interface{}, path []string) (interface{}, error) {
	// If overlay is nil, it means delete the key
	if overlay == nil {
		return nil, nil
	}

	// If base is nil, use overlay
	if base == nil {
		return deepCopyValue(overlay)
	}

	// Handle map merging
	baseMap, baseIsMap := base.(map[string]interface{})
	overlayMap, overlayIsMap := overlay.(map[string]interface{})

	if baseIsMap && overlayIsMap {
		result, err := deepCopyMap(baseMap)
		if err != nil {
			return nil, err
		}
		err = m.mergeIntoAtPath(result, overlayMap, path)
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	// Handle array merging
	baseArray, baseIsArray := base.([]interface{})
	overlayArray, overlayIsArray := overlay.([]interface{})

	if baseIsArray && overlayIsArray {
		// NOTE: arrays containing merge markers (append/prepend/merge/insert/
		// delete/replace/inline) never reach this branch: needsLegacyMerger
		// recursively detects them in the enclosing map and routes the whole
		// base/overlay pair through performLegacyMerge (the real merger),
		// which resolves markers identically with or without --skip-eval.
		switch m.arrayStrategy {
		case AppendArrays:
			// Append arrays
			result := make([]interface{}, len(baseArray)+len(overlayArray))
			copy(result, baseArray)
			copy(result[len(baseArray):], overlayArray)
			return result, nil
		case PrependArrays:
			// Prepend arrays
			result := make([]interface{}, len(overlayArray)+len(baseArray))
			copy(result, overlayArray)
			copy(result[len(overlayArray):], baseArray)
			return result, nil
		case ReplaceArrays:
			return deepCopyValue(overlayArray)
		case InlineArrays:
			return m.mergeArraysInlineAtPath(baseArray, overlayArray, path)
		default:
			// InlineArrays is the zero value, so an unset strategy lands in
			// the InlineArrays case above; any future strategy value falls
			// back to the same spruce-default positional merge.
			return m.mergeArraysInlineAtPath(baseArray, overlayArray, path)
		}
	}

	// For different types or scalars, overlay replaces base. If the
	// incoming scalar is a "(( calc <leading-op> ... ))" value-modification
	// expression, the base value it is about to overwrite is the "existing
	// value" that expression needs at evaluation time and can no longer
	// find at ev.Here once this replacement happens — record it (spec
	// cluster A5 §5.3).
	if overlayStr, ok := overlay.(string); ok && isCalcModificationExpression(overlayStr) {
		m.recordPriorCalcValue(path, base)
	}

	return deepCopyValue(overlay)
}

// mergeArraysInlineAtPath merges two arrays positionally, matching spruce's
// default (inline) list merge: overlay elements merge over base elements
// index by index, base elements beyond the overlay's length are kept, and
// overlay elements beyond the base's length are appended. Only arrays free
// of merge markers, maps, prune, and sort reach this path — anything else
// routes through the legacy merger (see needsLegacyMerger) — so elements
// here are scalars or nested plain arrays.
func (m *mergeBuilderImpl) mergeArraysInlineAtPath(baseArray, overlayArray []interface{}, path []string) ([]interface{}, error) {
	merged := make([]interface{}, 0, max(len(baseArray), len(overlayArray)))
	for i := range overlayArray {
		elemPath := append(append([]string{}, path...), strconv.Itoa(i))
		if i < len(baseArray) {
			elem, err := m.mergeValuesAtPath(baseArray[i], overlayArray[i], elemPath)
			if err != nil {
				return nil, err
			}
			merged = append(merged, elem)
		} else {
			copied, err := deepCopyValue(overlayArray[i])
			if err != nil {
				return nil, err
			}
			merged = append(merged, copied)
		}
	}
	for i := len(overlayArray); i < len(baseArray); i++ {
		copied, err := deepCopyValue(baseArray[i])
		if err != nil {
			return nil, err
		}
		merged = append(merged, copied)
	}
	return merged, nil
}

// isCalcModificationExpression reports whether s is an unevaluated "(( calc
// <leading-op> ... ))" value-modification expression — either the raw form
// ("(( calc * 2 ))") or the quoted form the parser normalizes it to
// ("(( calc "* 2" ))") — as opposed to an ordinary calc expression like
// "(( calc "1 + 2" ))", which needs no prior value. A leading +, -, *, /, %,
// or ^ character (op_calc.go's own leading-operator set, mirrored by the
// parser's raw-substring capture — spec §5.2) immediately after "calc"
// identifies the shape.
func isCalcModificationExpression(s string) bool {
	return calcModificationPattern.MatchString(strings.TrimSpace(s))
}

var calcModificationPattern = regexp.MustCompile(`^\(\(\s*calc\s+"?[*/+%^-]`)

// recordPriorCalcValue records value as the prior value at the canonical
// path (dot-joined, matching tree.Cursor.String()) about to be overwritten
// by a calc value-modification expression. A root-level path (path
// unset — should not occur, since calc's leading-operator form always
// targets a specific key) is ignored rather than recorded under an empty
// key.
func (m *mergeBuilderImpl) recordPriorCalcValue(path []string, value interface{}) {
	if len(path) == 0 {
		return
	}
	if m.priorCalcValues == nil {
		m.priorCalcValues = make(map[string]interface{})
	}
	m.priorCalcValues[strings.Join(path, ".")] = value
}

// hasArrayOperators checks if a map contains arrays with merge operators.
func (m *mergeBuilderImpl) hasArrayOperators(data map[string]interface{}) bool {
	for _, value := range data {
		if m.valueHasArrayOperators(value) {
			return true
		}
	}
	return false
}

// valueHasArrayOperators checks if a value (or its children) contains array operators.
func (m *mergeBuilderImpl) valueHasArrayOperators(value interface{}) bool {
	switch v := value.(type) {
	case []interface{}:
		if m.arrayHasOperators(v) {
			return true
		}
	case map[string]interface{}:
		if m.hasArrayOperators(v) {
			return true
		}
	case map[interface{}]interface{}:
		// yaml.v3 produces this for maps with non-string keys
		for _, val := range v {
			if m.valueHasArrayOperators(val) {
				return true
			}
		}
	}
	return false
}

// arrayHasOperators checks if an array contains merge operators.
func (m *mergeBuilderImpl) arrayHasOperators(array []interface{}) bool {
	for _, item := range array {
		if str, ok := item.(string); ok {
			// Check for any array modification operators
			if strings.Contains(str, "(( append ))") ||
				strings.Contains(str, "(( prepend ))") ||
				strings.Contains(str, "(( replace ))") ||
				strings.Contains(str, "(( inline ))") ||
				strings.Contains(str, "(( merge") || // matches (( merge )) and (( merge on key ))
				strings.Contains(str, "(( insert") || // matches various insert forms
				strings.Contains(str, "(( delete") { // matches various delete forms
				return true
			}
		}
	}
	return false
}

// hasArraysWithMaps checks if a map contains arrays with map elements (for merge-by-key detection).
func (m *mergeBuilderImpl) hasArraysWithMaps(data map[string]interface{}) bool {
	for _, value := range data {
		switch v := value.(type) {
		case []interface{}:
			for _, item := range v {
				if _, isMap := item.(map[string]interface{}); isMap {
					return true
				}
			}
		case map[string]interface{}:
			// Recursively check nested maps
			if m.hasArraysWithMaps(v) {
				return true
			}
		}
	}
	return false
}

// hasPruneOperators checks if a map contains prune operators.
func (m *mergeBuilderImpl) hasPruneOperators(data map[string]interface{}) bool {
	for _, value := range data {
		switch v := value.(type) {
		case string:
			// Check if it's a prune operator
			if strings.TrimSpace(v) == "(( prune ))" {
				return true
			}
		case map[string]interface{}:
			// Recursively check nested maps
			if m.hasPruneOperators(v) {
				return true
			}
		case []interface{}:
			// Check arrays for prune operators
			for _, item := range v {
				if str, ok := item.(string); ok && strings.TrimSpace(str) == "(( prune ))" {
					return true
				}
				// Also check if array contains maps with prune operators
				if mapItem, ok := item.(map[string]interface{}); ok {
					if m.hasPruneOperators(mapItem) {
						return true
					}
				}
			}
		}
	}
	return false
}

// hasSortOperators checks if a map contains sort operators.
func (m *mergeBuilderImpl) hasSortOperators(data map[string]interface{}) bool {
	for _, value := range data {
		switch v := value.(type) {
		case string:
			// Check if it's a sort operator
			trimmed := strings.TrimSpace(v)
			if strings.HasPrefix(trimmed, "(( sort") && strings.HasSuffix(trimmed, "))") {
				return true
			}
		case map[string]interface{}:
			// Recursively check nested maps
			if m.hasSortOperators(v) {
				return true
			}
		}
	}
	return false
}

// isMergerError checks if an error originated from the legacy merger
// (performLegacyMerge/convertMergerError), which always reports detailed,
// per-path failures (bad key-merge, out-of-bounds insert/delete index,
// duplicate insert entry, missing modification point, etc.) as a *MultiError.
// Those messages must reach the CLI unwrapped, matching spruce's own
// "N error(s) detected: - $.path: ..." formatting, rather than being
// flattened into a generic "failed to merge documents" wrapper.
func isMergerError(err error) bool {
	if err == nil {
		return false
	}
	var multiErr *MultiError
	return errors.As(err, &multiErr)
}

// applyPostProcessing applies pruning, cherry-picking, and evaluation.
func (m *mergeBuilderImpl) applyPostProcessing(doc Document) (Document, error) {
	result := doc

	// go-patch operations are applied inline, at their position in the file
	// sequence, by mergeDocuments; nothing left to do for them here.

	// Pass merge metadata to engine before evaluation
	if m.mergeMetadata != nil && m.engine != nil {
		// Add prune paths to engine state
		for _, path := range m.mergeMetadata.PrunePaths {
			m.engine.GetOperatorState().AddKeyToPrune(path)
		}
		// Add sort paths to engine state
		for path, order := range m.mergeMetadata.SortPaths {
			m.engine.GetOperatorState().AddPathToSort(path, order)
		}
	}

	// Apply evaluation if not skipped
	if !m.skipEvaluation {
		evaluated, err := m.applyEvaluation(result)
		if err != nil {
			return nil, err
		}
		result = evaluated
	} else if m.engine != nil {
		// (( sort by X )) list ordering is applied by spruce even when
		// evaluation is skipped (--skip-eval only disables scalar operator
		// evaluation, not merge-phase list ordering). The normal path applies
		// queued sort paths as evaluator post-processing (see engine.go
		// evaluate()), which is bypassed here, so apply them directly.
		sorted, err := m.applySortPathsWithoutEvaluation(result)
		if err != nil {
			return nil, err
		}
		result = sorted
	}

	// Collect prune keys from both sources:
	// 1. Keys specified via --prune flag (m.pruneKeys)
	// 2. Keys marked for pruning during evaluation via (( prune )) operator
	allPruneKeys := make([]string, len(m.pruneKeys))
	copy(allPruneKeys, m.pruneKeys)

	if m.engine != nil {
		// Add keys marked for pruning during evaluation
		evalPruneKeys := m.engine.GetOperatorState().GetKeysToPrune()
		allPruneKeys = append(allPruneKeys, evalPruneKeys...)
	}

	// __graft_loop is a reserved top-level key the control-flow preprocessor
	// (pkg/graft/controlflow) uses to materialize for-loop variables so
	// generated bodies can (( grab )) them during normal evaluation. It must
	// never reach evaluated output. Gated on actually being present (a cheap
	// top-level map lookup, not a tree walk) rather than appended
	// unconditionally: an unconditional append would flip applyPruning's
	// "anything to prune?" check to true for every document, forcing its
	// deepCopyMap over documents that never touched control flow and have no
	// other reason to be pruned or copied. Also gated on evaluation having
	// run: under --skip-eval the loop bodies still hold unresolved
	// (( grab __graft_loop... )) references, and dropping the bindings would
	// strand every one of them in an intermediate meant to be merged again.
	if !m.skipEvaluation {
		if resultData, ok := result.RawData().(map[string]interface{}); ok {
			if _, present := resultData["__graft_loop"]; present {
				allPruneKeys = append(allPruneKeys, "__graft_loop")
			}
		}
	}

	// Apply pruning AFTER evaluation so that grab operators can reference values before they're pruned
	if len(allPruneKeys) > 0 {
		// Temporarily set m.pruneKeys to all keys for the applyPruning method
		originalPruneKeys := m.pruneKeys
		m.pruneKeys = allPruneKeys
		var pruneErr error
		result, pruneErr = m.applyPruning(result)
		m.pruneKeys = originalPruneKeys // Restore original
		if pruneErr != nil {
			return nil, pruneErr
		}
	}

	// Apply cherry-picking AFTER evaluation and pruning
	if len(m.cherryPickKeys) > 0 {
		cherryPicked, err := m.applyCherryPicking(result)
		if err != nil {
			return nil, err
		}
		result = cherryPicked
	}

	return result, nil
}

// applyGoPatchToMap applies a single set of go-patch operations to data at
// its position in the merge sequence (see mergeDocuments), converting to and
// from go-patch's map[interface{}]interface{} convention (yaml.v2 style)
// around the call.
func applyGoPatchToMap(data map[string]interface{}, ops patch.Ops) (map[string]interface{}, error) {
	converted := toInterfaceKeyMap(data)

	result, err := ops.Apply(converted)
	if err != nil {
		return nil, err
	}

	// Convert the result back to map[string]interface{} for the rest of the pipeline
	switch typed := result.(type) {
	case map[string]interface{}:
		return NormalizeMap(typed), nil
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, v := range typed {
			out[fmt.Sprintf("%v", k)] = v
		}
		return NormalizeMap(out), nil
	default:
		return nil, fmt.Errorf("go-patch operations resulted in non-map data")
	}
}

// toInterfaceKeyMap deep-converts map[string]interface{} to
// map[interface{}]interface{} for go-patch compatibility.
func toInterfaceKeyMap(data map[string]interface{}) map[interface{}]interface{} {
	result := make(map[interface{}]interface{}, len(data))
	for k, v := range data {
		result[k] = toInterfaceKeyValue(v)
	}
	return result
}

func toInterfaceKeyValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return toInterfaceKeyMap(val)
	case map[interface{}]interface{}:
		// Already interface keys, but recurse into values
		for k, v := range val {
			val[k] = toInterfaceKeyValue(v)
		}
		return val
	case []interface{}:
		for i, item := range val {
			val[i] = toInterfaceKeyValue(item)
		}
		return val
	default:
		return v
	}
}

// applySortPathsWithoutEvaluation applies queued (( sort by X )) list ordering
// directly to the document without running the full evaluator, covering the
// skip-eval path where engine.go's evaluate() (and its sort post-processing)
// never runs. Unlike evaluate(), unresolvable or non-list sort targets are
// skipped here rather than treated as errors. A cyclic document is treated
// as an error, same as every other deepCopyMap call site.
func (m *mergeBuilderImpl) applySortPathsWithoutEvaluation(doc Document) (Document, error) {
	sortPaths := m.engine.GetOperatorState().GetPathsToSort()
	if len(sortPaths) == 0 {
		return doc, nil
	}

	data, ok := doc.RawData().(map[string]interface{})
	if !ok {
		return doc, nil // Return unchanged if not a map
	}
	result, err := deepCopyMap(data)
	if err != nil {
		return nil, err
	}

	for path, sortKey := range sortPaths {
		cleanPath := strings.TrimPrefix(path, "$.")
		value, err := getValueAtPath(result, cleanPath)
		if err != nil {
			// Path no longer resolves (e.g. pruned); nothing to sort.
			continue
		}
		list, isList := value.([]interface{})
		if !isList {
			// Sort marker resolved to a non-list value; nothing to sort.
			continue
		}
		if err := SortList(cleanPath, list, sortKey); err != nil {
			// Preserve unsorted order rather than fail the whole merge;
			// matches engine.go's evaluate(), which logs and continues.
			continue
		}
	}

	m.engine.GetOperatorState().ResetPathsToSort()
	return NewDocument(result), nil
}

// applyPruning removes specified keys from the document.
//
// A prune path that does not resolve to anything (missing key,
// out-of-range index, or a named array-entry lookup at the final path
// segment) is not an error: removeKey returns nil for all of those
// cases, matching spruce's own --prune behavior (verified against the
// spruce binary), where an unresolved path is silently skipped. Any
// non-nil error from removeKey is therefore a genuine failure and is
// propagated rather than swallowed.
func (m *mergeBuilderImpl) applyPruning(doc Document) (Document, error) {
	data, ok := doc.RawData().(map[string]interface{})
	if !ok {
		return doc, nil // Return unchanged if not a map
	}
	result, err := deepCopyMap(data)
	if err != nil {
		return nil, err
	}

	for _, key := range m.pruneKeys {
		if err := m.removeKey(result, key); err != nil {
			return nil, fmt.Errorf("prune %q: %w", key, err)
		}
	}

	return NewDocument(result), nil
}

// applyCherryPicking keeps only specified keys in the document.
//
//nolint:gocyclo // cherry-picking handles arrays, maps, and nested paths
func (m *mergeBuilderImpl) applyCherryPicking(doc Document) (Document, error) {
	data, ok := doc.RawData().(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("document data is not a map")
	}
	result := make(map[string]interface{})

	// Group cherry-pick paths by their parent
	type arraySelection struct {
		indices map[int]bool
		names   map[string]bool
	}
	arrayPaths := make(map[string]*arraySelection)
	regularPaths := []string{}

	// First pass: categorize and group paths
	for _, keyPath := range m.cherryPickKeys {
		parts := strings.Split(keyPath, ".")

		if len(parts) >= 2 {
			parentPath := strings.Join(parts[:len(parts)-1], ".")
			lastPart := parts[len(parts)-1]

			parentValue, err := m.extractKey(data, parentPath)
			if err == nil {
				if arr, isArray := parentValue.([]interface{}); isArray {
					// This is an array - check if we can handle it
					if idx, isNum := isNumericIndex(lastPart); isNum {
						if idx >= 0 && idx < len(arr) {
							// Valid numeric index
							if arrayPaths[parentPath] == nil {
								arrayPaths[parentPath] = &arraySelection{
									indices: make(map[int]bool),
									names:   make(map[string]bool),
								}
							}
							arrayPaths[parentPath].indices[idx] = true
							continue
						}
					} else {
						// Try named lookup
						_, found := findNamedArrayEntry(arr, lastPart)
						if found {
							if arrayPaths[parentPath] == nil {
								arrayPaths[parentPath] = &arraySelection{
									indices: make(map[int]bool),
									names:   make(map[string]bool),
								}
							}
							arrayPaths[parentPath].names[lastPart] = true
							continue
						}
					}
				}
			}
		}

		// Not an array path or couldn't handle it
		regularPaths = append(regularPaths, keyPath)
	}

	// Second pass: extract array elements in their original order
	for parentPath, selection := range arrayPaths {
		parentValue, _ := m.extractKey(data, parentPath)
		arr, ok := parentValue.([]interface{})
		if !ok {
			continue // Skip if not an array
		}

		selectedItems := []interface{}{}

		// Iterate through the array in reverse order
		// This matches the expected test behavior where higher indices come first
		for i := len(arr) - 1; i >= 0; i-- {
			item := arr[i]
			// Check if this index is selected
			if selection.indices[i] {
				selectedItems = append(selectedItems, item)
				continue
			}

			// Check if this item has a name that's selected
			if len(selection.names) > 0 {
				for name := range selection.names {
					if entry, found := findNamedArrayEntry([]interface{}{item}, name); found {
						selectedItems = append(selectedItems, entry)
						break
					}
				}
			}
		}

		err := m.setKey(result, parentPath, selectedItems)
		if err != nil {
			return nil, err
		}
	}

	// Handle regular paths
	for _, path := range regularPaths {
		value, err := m.extractKey(data, path)
		if err != nil {
			// Special handling for array access with invalid indices
			parts := strings.Split(path, ".")
			if len(parts) >= 2 {
				parentPath := strings.Join(parts[:len(parts)-1], ".")
				parentValue, parentErr := m.extractKey(data, parentPath)
				if parentErr == nil {
					if _, isArray := parentValue.([]interface{}); isArray {
						// Parent is an array, but we couldn't access the element
						// Create a nested map structure: parent.key = entire array
						lastPart := parts[len(parts)-1]
						mapWithKey := map[string]interface{}{
							lastPart: parentValue,
						}
						err = m.setKey(result, parentPath, mapWithKey)
						if err != nil {
							return nil, err
						}
						continue
					}
				}
			}
			// For other errors, return the error
			return nil, err
		}
		err = m.setKey(result, path, value)
		if err != nil {
			return nil, err
		}
	}

	return NewDocument(result), nil
}

// applyEvaluation runs operator evaluation on the document.
func (m *mergeBuilderImpl) applyEvaluation(doc Document) (Document, error) {
	// Use the engine's evaluate method if available
	if m.engine != nil {
		evalCtx := m.ctx
		// If we have cherry-pick keys, pass them to the engine for evaluation
		if len(m.cherryPickKeys) > 0 {
			evalCtx = WithCherryPickPaths(evalCtx, m.cherryPickKeys)
		}
		// Pass calc value-modification prior values recorded during merge,
		// if any.
		if len(m.priorCalcValues) > 0 {
			evalCtx = WithPriorCalcValues(evalCtx, m.priorCalcValues)
		}
		return m.engine.Evaluate(evalCtx, doc)
	}

	// Fallback: create basic evaluator (this should not happen in practice)
	data, ok := doc.RawData().(map[string]interface{})
	if !ok {
		return nil, NewEvaluationError("", "document data is not a map", nil)
	}

	// Create evaluator
	evaluator := &Evaluator{
		Tree:        data,
		PriorValues: m.priorCalcValues,
	}

	// Run evaluation - pass cherry-pick keys as the "picks" parameter
	err := evaluator.Run(nil, m.cherryPickKeys)
	if err != nil {
		return nil, NewEvaluationError("", "failed to evaluate merged document", err)
	}

	return NewDocument(evaluator.Tree), nil
}

// Helper functions for key manipulation

//nolint:gocyclo // removeKey navigates nested maps and arrays with various path formats
func (m *mergeBuilderImpl) removeKey(data map[string]interface{}, keyPath string) error {
	// Handle nested paths like "config.enabled" or "meta.list.1"
	if keyPath == "" {
		return nil
	}

	// Split path by dots
	parts := strings.Split(keyPath, ".")
	if len(parts) == 1 {
		// Simple key
		delete(data, keyPath)
		return nil
	}

	// Navigate to the parent of the target, tracking the immediate container
	// (map or array) that holds `current` and the key/index used to reach it
	// at every step. Unlike a re-navigation from the root, this walk covers
	// map segments *and* array segments (numeric index or named entry) at
	// every position, so the final-segment array branch below can splice the
	// target array back into whatever holds it — a map field, or a slot in
	// an outer array — for paths like "jobs.0.networks.1" or
	// "jobs.web.networks.1" that pass through an array before the final
	// segment.
	var current interface{} = data
	var parentContainer interface{}
	var parentKey interface{} // string for a map parent, int for an array parent

	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]

		switch v := current.(type) {
		case map[string]interface{}:
			value, exists := v[part]
			if !exists {
				// Path doesn't exist, nothing to remove
				return nil
			}
			parentContainer, parentKey = v, part
			current = value
		case []interface{}:
			// Handle array index or named entry
			if idx, isNum := isNumericIndex(part); isNum {
				// Numeric index
				if idx < 0 || idx >= len(v) {
					// Index out of bounds, nothing to remove
					return nil
				}
				parentContainer, parentKey = v, idx
				current = v[idx]
			} else {
				// Named entry lookup
				idx, entry, found := findNamedArrayEntryWithIndex(v, part)
				if !found {
					// Named entry not found, nothing to remove
					return nil
				}
				parentContainer, parentKey = v, idx
				current = entry
			}
		default:
			// current is a scalar (or other non-container) but the path has
			// more segments to traverse: the target doesn't exist under it.
			// Matches spruce, where tree.Cursor.Resolve returns an error in
			// this situation and the caller silently skips the prune.
			return nil
		}
	}

	// Now remove the final key/index
	finalPart := parts[len(parts)-1]

	switch parent := current.(type) {
	case map[string]interface{}:
		// Simple map key deletion
		delete(parent, finalPart)
	case []interface{}:
		// Array index deletion. spruce's own --prune only removes array
		// entries addressed by a numeric index at the final path segment;
		// a bare named final segment (e.g. "items.beta") is a no-op there
		// too (verified against the spruce binary: named lookup is only
		// used by spruce when navigating *through* an array to reach a
		// deeper key, never as the final delete target). Mirror that for
		// the bare-name case, which has a real spruce behavior to stay
		// compatible with.
		//
		// A "field=value" predicate final segment (e.g.
		// "servers.name=secondary") is different: spruce has no predicate
		// syntax at all, so there is no spruce
		// behavior to preserve, and the whole point of wiring predicates
		// into --cherry-pick/--prune is that they resolve to a specific
		// array entry — so a predicate final segment does resolve to a
		// delete index, via the same tree.PredicateFind the resolver uses.
		index, isNum := isNumericIndex(finalPart)
		if !isNum {
			if field, value, isPredicate := tree.ParsePredicateSegment(finalPart); isPredicate {
				if _, idx, found := tree.PredicateFind(parent, field, value); found {
					index, isNum = int(idx), true
				}
			}
		}
		if !isNum {
			return nil
		}
		if index < 0 || index >= len(parent) {
			// Index out of bounds, nothing to remove. The spruce binary
			// panics on an out-of-range --prune index into an array; graft
			// intentionally diverges here and stays graceful, matching its
			// own no-op-on-unresolved-path behavior for every other prune
			// shape rather than crashing the whole merge.
			return nil
		}

		// Splice the element out and write the shortened array back into the
		// map field that holds it. spruce's own Prune() (evaluator.go) only
		// performs this write-back when the array's own container is a map
		// (its `reflect.TypeOf(s).Kind() == reflect.Map` check); an array
		// nested directly inside another array with no intervening map (e.g.
		// prune path "matrix.0.1" on `matrix: [[10,20,30],[40,50,60]]`) is a
		// silent no-op in spruce too (verified against the binary). Mirror
		// that restriction exactly — parentKey/parentContainer is populated
		// for every intermediate segment (map field, numeric array index, or
		// named array entry) so paths like "jobs.0.networks.1" and
		// "jobs.web.networks.1" reach a map parentContainer correctly; only
		// a bare array-of-arrays final segment stays unsupported, matching
		// spruce rather than silently going further than it does.
		if pc, ok := parentContainer.(map[string]interface{}); ok {
			if key, ok := parentKey.(string); ok {
				newArr := append(append([]interface{}{}, parent[:index]...), parent[index+1:]...)
				pc[key] = newArr
			}
		}
	default:
		// The path navigates down to a scalar (or other non-container)
		// and still expects a final key/index under it: nothing to
		// remove. Matches spruce's default case in Prune(), which logs
		// and moves on without deleting anything or raising an error.
		return nil
	}

	return nil
}

// isNumericIndex checks if a string is a valid numeric array index.
func isNumericIndex(s string) (int, bool) {
	idx, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return idx, true
}

// findNamedArrayEntry searches for an entry in an array by checking identifier fields.
// The configured array-merge identifier key (DEFAULT_ARRAY_MERGE_KEY, "name" by
// default) is checked first and is authoritative; "name", "id", and "key" remain
// as fallbacks for entries that don't use the configured key, preserving prior
// behavior for documents that don't set the environment variable.
func findNamedArrayEntry(arr []interface{}, name string) (interface{}, bool) {
	_, entry, found := findNamedArrayEntryWithIndex(arr, name)
	return entry, found
}

// findNamedArrayEntryWithIndex is findNamedArrayEntry plus the position of
// the matched entry within arr. removeKey needs the index (not just the
// entry) to splice a shortened array back into its parent container when
// navigation passes through a named array entry before reaching the final
// prune target.
//
// A "field=value" path segment (e.g. the "servers.name=primary" of
// --cherry-pick/--prune) is a list predicate, not a name/id/key lookup —
// dispatched to tree.PredicateFind so cherry-pick and prune match the exact
// same first-match, list-containers-only semantics as (( grab ... ))
// predicate resolution, carried into this path
// navigator, which used to hand-roll its own name-field-only search and
// silently fail on predicate syntax).
func findNamedArrayEntryWithIndex(arr []interface{}, name string) (int, interface{}, bool) {
	if field, value, isPredicate := tree.ParsePredicateSegment(name); isPredicate {
		entry, idx, found := tree.PredicateFind(arr, field, value)
		if !found {
			return -1, nil, false
		}
		return int(idx), entry, true
	}

	configuredKey := merger.GetIdentifierKey()
	identifierKeys := make([]string, 0, 4)
	identifierKeys = append(identifierKeys, configuredKey)
	for _, fallback := range []string{"name", "id", "key"} {
		if fallback != configuredKey {
			identifierKeys = append(identifierKeys, fallback)
		}
	}

	for i, entry := range arr {
		// Only check map entries
		switch v := entry.(type) {
		case map[string]interface{}:
			for _, idKey := range identifierKeys {
				if val, exists := v[idKey]; exists && fmt.Sprintf("%v", val) == name {
					return i, entry, true
				}
			}
		}
	}

	return -1, nil, false
}

func (m *mergeBuilderImpl) extractKey(data map[string]interface{}, keyPath string) (interface{}, error) {
	// Handle nested paths like "config.enabled"
	if keyPath == "" {
		return nil, NewValidationError("empty key path")
	}

	// Split path by dots
	parts := strings.Split(keyPath, ".")

	// Navigate through the structure
	var current interface{} = data
	for i, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			value, exists := v[part]
			if !exists {
				return nil, NewValidationError(fmt.Sprintf("key not found: %s (missing segment '%s')", keyPath, part))
			}
			current = value
		case []interface{}:
			// Handle array access
			if idx, isNum := isNumericIndex(part); isNum {
				// Numeric index access
				if idx < 0 || idx >= len(v) {
					return nil, NewValidationError(fmt.Sprintf("array index out of bounds: %s (index %d, array length %d)", keyPath, idx, len(v)))
				}
				current = v[idx]
			} else {
				// Named entry lookup
				entry, found := findNamedArrayEntry(v, part)
				if !found {
					return nil, NewValidationError(fmt.Sprintf("named array entry not found: %s (looking for '%s')", keyPath, part))
				}
				current = entry
			}
		default:
			if i < len(parts)-1 {
				// Still have more path segments but current value is not navigable
				return nil, NewValidationError(fmt.Sprintf("cannot navigate path '%s' at segment %d: '%s' is not a map or array", keyPath, i, parts[i-1]))
			}
		}
	}

	return deepCopyValue(current)
}

func (m *mergeBuilderImpl) setKey(data map[string]interface{}, keyPath string, value interface{}) error {
	// Handle nested paths like "config.enabled"
	if keyPath == "" {
		return NewValidationError("empty key path")
	}

	// Split path by dots
	parts := strings.Split(keyPath, ".")

	// For simple keys, just set directly
	if len(parts) == 1 {
		data[keyPath] = value
		return nil
	}

	// Navigate to the parent map and set the final key
	current := data
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]

		if next, exists := current[part]; exists {
			switch v := next.(type) {
			case map[string]interface{}:
				current = v
			default:
				return NewValidationError(fmt.Sprintf("cannot set path '%s': segment '%s' is not a map", keyPath, part))
			}
		} else {
			// Create intermediate maps as needed
			newMap := make(map[string]interface{})
			current[part] = newMap
			current = newMap
		}
	}

	// Set the final value
	finalKey := parts[len(parts)-1]
	current[finalKey] = value
	return nil
}

// detectCyclicValue reports whether v — a document's raw
// map[string]interface{} data, or any value nested inside it — contains a
// map or list that (transitively) contains itself. It walks the same two
// container shapes deepCopyMap/deepCopyValue and this file's
// hasArrayOperators/hasArraysWithMaps/hasPruneOperators/hasSortOperators
// walkers recurse into, without allocating a copy, so Execute can reject a
// cyclic document in one pass before any of those unrelated walkers get a
// chance to run on it and overflow the stack themselves. Uses the same
// on-stack (not permanent-seen) pointer tracking as deepCopyMapOnStack for
// the same reason: a diamond-shaped, shared-but-acyclic structure must not
// be misreported as cyclic.
func detectCyclicValue(v interface{}) error {
	return detectCyclicValueOnStack(v, make(map[uintptr]struct{}))
}

func detectCyclicValueOnStack(v interface{}, onStack map[uintptr]struct{}) error {
	switch val := v.(type) {
	case map[string]interface{}:
		if len(val) == 0 {
			return nil
		}
		ptr := reflect.ValueOf(val).Pointer()
		if _, cyclic := onStack[ptr]; cyclic {
			return fmt.Errorf("cyclic reference detected: a map contains itself")
		}
		onStack[ptr] = struct{}{}
		defer delete(onStack, ptr)
		for _, item := range val {
			if err := detectCyclicValueOnStack(item, onStack); err != nil {
				return err
			}
		}
	case []interface{}:
		if len(val) == 0 {
			return nil
		}
		ptr := reflect.ValueOf(val).Pointer()
		if _, cyclic := onStack[ptr]; cyclic {
			return fmt.Errorf("cyclic reference detected: a list contains itself")
		}
		onStack[ptr] = struct{}{}
		defer delete(onStack, ptr)
		for _, item := range val {
			if err := detectCyclicValueOnStack(item, onStack); err != nil {
				return err
			}
		}
	}
	return nil
}

// Deep copy helpers
//
// Both functions track the map/list pointers currently on the recursion
// stack (not every pointer ever seen) so a self-referencing value — a map
// or list that transitively contains itself — is reported as an error
// instead of recursing forever and overflowing the goroutine stack. Using
// an on-stack set rather than a permanent "seen" set is required for
// correctness, not just style: a diamond-shaped structure (the same map or
// list instance legitimately reachable from two different sibling
// branches) must still copy successfully, and it will revisit that
// instance's pointer after the first branch has already finished with it
// and popped it back off the stack.
//
// Every caller in this file can carry an error (they already return one
// for other merge failures), so a cycle is always surfaced as an error
// here rather than silently truncated.

func deepCopyMap(src map[string]interface{}) (map[string]interface{}, error) {
	return deepCopyMapOnStack(src, make(map[uintptr]struct{}))
}

func deepCopyMapOnStack(src map[string]interface{}, onStack map[uintptr]struct{}) (map[string]interface{}, error) {
	// An empty (or nil) map cannot contain itself: there is nothing to
	// recurse into, so it never needs a stack entry. Skipping it also
	// avoids collisions between distinct empty/nil maps, which is Go
	// pointer arithmetic can share a zero-value representation for.
	if len(src) > 0 {
		ptr := reflect.ValueOf(src).Pointer()
		if _, cyclic := onStack[ptr]; cyclic {
			return nil, fmt.Errorf("cyclic reference detected while copying: a map contains itself")
		}
		onStack[ptr] = struct{}{}
		defer delete(onStack, ptr)
	}

	dst := make(map[string]interface{}, len(src))
	for key, value := range src {
		copied, err := deepCopyValueOnStack(value, onStack)
		if err != nil {
			return nil, err
		}
		dst[key] = copied
	}
	return dst, nil
}

func deepCopyValue(src interface{}) (interface{}, error) {
	return deepCopyValueOnStack(src, make(map[uintptr]struct{}))
}

func deepCopyValueOnStack(src interface{}, onStack map[uintptr]struct{}) (interface{}, error) {
	switch v := src.(type) {
	case map[string]interface{}:
		return deepCopyMapOnStack(v, onStack)
	case []interface{}:
		if len(v) > 0 {
			ptr := reflect.ValueOf(v).Pointer()
			if _, cyclic := onStack[ptr]; cyclic {
				return nil, fmt.Errorf("cyclic reference detected while copying: a list contains itself")
			}
			onStack[ptr] = struct{}{}
			defer delete(onStack, ptr)
		}
		dst := make([]interface{}, len(v))
		for i, item := range v {
			copied, err := deepCopyValueOnStack(item, onStack)
			if err != nil {
				return nil, err
			}
			dst[i] = copied
		}
		return dst, nil
	default:
		return v, nil // Primitives are copied by value
	}
}

// valuesEqual performs a simple equality check for values.
func valuesEqual(a, b interface{}) bool {
	// Handle nil cases
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// For maps and slices, use fmt.Sprintf comparison as a simple deep equality check
	// This is not the most efficient but handles nested structures
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
