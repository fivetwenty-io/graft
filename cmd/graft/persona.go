package main

// persona is the tool a graft invocation speaks for. The graft persona
// is graft's own command line, and the others are the tools graft
// stands in for, whose argument rules and messages it follows when it
// runs under their names.
type persona int

// The personas.
const (
	personaGraft persona = iota
	personaSpruce
	personaJQ
	personaYQ
)
