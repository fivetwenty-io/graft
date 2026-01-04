// Package postprocess provides post-processing handlers for graft documents.
//
// Post-processors run after operator evaluation to perform final transformations
// on the merged document. They execute in a defined order based on their phase.
//
// # Built-in Post-Processors
//
// Pruning:
//   - Remove fields marked with (( prune ))
//   - Clean up temporary/intermediate values
//
// Cherry-picking:
//   - Extract specific subtrees from documents
//   - Support for multiple path selection
//
// Sorting:
//   - Sort map keys alphabetically
//   - Sort arrays by specified criteria
//
// Validation:
//   - Schema validation
//   - Required field checking
//   - Type validation
//
// # Post-Processing Phases
//
// Post-processors execute in phases:
//
//	const (
//	    PhasePrune     PostProcessPhase = iota  // Remove pruned fields
//	    PhaseTransform                          // Apply transformations
//	    PhaseValidate                           // Validate final document
//	)
//
// # Custom Post-Processors
//
// Implement the PostProcessor interface:
//
//	type PostProcessor interface {
//	    Name() string
//	    Phase() PostProcessPhase
//	    Process(ctx context.Context, doc *Document, meta *Metadata) error
//	}
//
// Register with the engine:
//
//	engine := graft.NewEngine(
//	    graft.WithPostProcessors(&MyPostProcessor{}),
//	)
package postprocess
