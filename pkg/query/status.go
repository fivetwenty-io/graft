package query

// Status is how a query command ends, which is the exit code it returns and
// the diagnostic it prints first. An empty Message prints nothing.
type Status struct {
	Code    int
	Message string
}
