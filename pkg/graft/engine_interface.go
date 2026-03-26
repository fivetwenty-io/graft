package graft

// GetEngine returns the engine from an evaluator.
// If no engine is set, a default engine is created for backward compatibility.
func GetEngine(ev *Evaluator) Engine {
	if ev.engine != nil {
		return ev.engine
	}
	// Return a default engine for backward compatibility
	engine, _ := CreateDefaultEngine()
	return engine
}
