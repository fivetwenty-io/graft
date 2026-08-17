package interfaces

import (
	"sort"
	"strconv"
	"sync"
	"testing"
)

// Test constants for repeated string literals.
const (
	testOpModulo = "modulo"
)

// TestOperatorPhaseString tests the String method of OperatorPhase.
func TestOperatorPhaseString(t *testing.T) {
	tests := []struct {
		phase    OperatorPhase
		expected string
	}{
		{MergePhase, "merge"},
		{ParamPhase, "param"},
		{EvalPhase, "eval"},
		{OperatorPhase(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.phase.String(); got != tt.expected {
				t.Errorf("OperatorPhase(%d).String() = %q, want %q", tt.phase, got, tt.expected)
			}
		})
	}
}

// TestOperatorCategoryString tests the String method of OperatorCategory.
func TestOperatorCategoryString(t *testing.T) {
	tests := []struct {
		category OperatorCategory
		expected string
	}{
		{CategoryData, "data"},
		{CategoryArithmetic, "arithmetic"},
		{CategoryString, "string"},
		{CategoryLogic, "logic"},
		{CategoryComparison, "comparison"},
		{CategoryArray, "array"},
		{CategoryControl, "control"},
		{CategoryExternal, "external"},
		{CategoryType, "type"},
		{CategoryIP, "ip"},
		{OperatorCategory(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.category.String(); got != tt.expected {
				t.Errorf("OperatorCategory(%d).String() = %q, want %q", tt.category, got, tt.expected)
			}
		})
	}
}

// TestOperatorRegistryBasic tests basic registry operations.
func TestOperatorRegistryBasic(t *testing.T) {
	registry := NewOperatorRegistry()

	// Register an operator
	info := OperatorInfo{
		Name:        "test",
		MinArgs:     1,
		MaxArgs:     2,
		Description: "Test operator",
		Category:    CategoryData,
		Phase:       EvalPhase,
	}
	registry.Register("test", info)

	// Look it up
	found, ok := registry.Lookup("test")
	if !ok {
		t.Fatal("expected to find registered operator")
	}

	if found.Name != "test" {
		t.Errorf("Name = %q, want %q", found.Name, "test")
	}
	if found.MinArgs != 1 {
		t.Errorf("MinArgs = %d, want 1", found.MinArgs)
	}
	if found.MaxArgs != 2 {
		t.Errorf("MaxArgs = %d, want 2", found.MaxArgs)
	}
	if found.Description != "Test operator" {
		t.Errorf("Description = %q, want %q", found.Description, "Test operator")
	}
	if found.Category != CategoryData {
		t.Errorf("Category = %v, want %v", found.Category, CategoryData)
	}
	if found.Phase != EvalPhase {
		t.Errorf("Phase = %v, want %v", found.Phase, EvalPhase)
	}
}

// TestOperatorRegistryAliases tests alias lookup.
func TestOperatorRegistryAliases(t *testing.T) {
	registry := NewOperatorRegistry()

	info := OperatorInfo{
		Name:        testOpModulo,
		MinArgs:     2,
		MaxArgs:     2,
		Description: "Modulo operator",
		Category:    CategoryArithmetic,
		Phase:       EvalPhase,
		Aliases:     []string{"mod", "rem"},
	}
	registry.Register(testOpModulo, info)

	// Look up by canonical name
	found, ok := registry.Lookup(testOpModulo)
	if !ok {
		t.Fatal("expected to find by canonical name")
	}
	if found.Name != testOpModulo {
		t.Errorf("Name = %q, want %q", found.Name, testOpModulo)
	}

	// Look up by alias
	found, ok = registry.Lookup("mod")
	if !ok {
		t.Fatal("expected to find by alias 'mod'")
	}
	if found.Name != testOpModulo {
		t.Errorf("Name = %q, want %q", found.Name, testOpModulo)
	}

	found, ok = registry.Lookup("rem")
	if !ok {
		t.Fatal("expected to find by alias 'rem'")
	}
	if found.Name != testOpModulo {
		t.Errorf("Name = %q, want %q", found.Name, testOpModulo)
	}

	// Lookup non-existent
	_, ok = registry.Lookup("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent operator")
	}
}

// TestOperatorRegistryList tests listing operators.
func TestOperatorRegistryList(t *testing.T) {
	registry := NewOperatorRegistry()

	registry.Register("alpha", OperatorInfo{Category: CategoryData})
	registry.Register("gamma", OperatorInfo{Category: CategoryString})
	registry.Register("beta", OperatorInfo{Category: CategoryData})

	list := registry.List()

	// Should be sorted
	expected := []string{"alpha", "beta", "gamma"}
	if len(list) != len(expected) {
		t.Fatalf("List() returned %d items, want %d", len(list), len(expected))
	}

	for i, name := range expected {
		if list[i] != name {
			t.Errorf("List()[%d] = %q, want %q", i, list[i], name)
		}
	}
}

// TestOperatorRegistryListByCategory tests listing operators by category.
func TestOperatorRegistryListByCategory(t *testing.T) {
	registry := NewOperatorRegistry()

	registry.Register("grab", OperatorInfo{Category: CategoryData})
	registry.Register("concat", OperatorInfo{Category: CategoryString})
	registry.Register("prune", OperatorInfo{Category: CategoryData})
	registry.Register("upper", OperatorInfo{Category: CategoryString})

	dataOps := registry.ListByCategory(CategoryData)
	expected := []string{"grab", "prune"}
	if len(dataOps) != len(expected) {
		t.Fatalf("ListByCategory(Data) returned %d items, want %d", len(dataOps), len(expected))
	}
	sort.Strings(expected)
	for i, name := range expected {
		if dataOps[i] != name {
			t.Errorf("ListByCategory(Data)[%d] = %q, want %q", i, dataOps[i], name)
		}
	}

	stringOps := registry.ListByCategory(CategoryString)
	expected = []string{"concat", "upper"}
	sort.Strings(expected)
	if len(stringOps) != len(expected) {
		t.Fatalf("ListByCategory(String) returned %d items, want %d", len(stringOps), len(expected))
	}
	for i, name := range expected {
		if stringOps[i] != name {
			t.Errorf("ListByCategory(String)[%d] = %q, want %q", i, stringOps[i], name)
		}
	}

	// Empty category
	ipOps := registry.ListByCategory(CategoryIP)
	if len(ipOps) != 0 {
		t.Errorf("ListByCategory(IP) returned %d items, want 0", len(ipOps))
	}
}

// TestOperatorRegistryListByPhase tests listing operators by phase.
func TestOperatorRegistryListByPhase(t *testing.T) {
	registry := NewOperatorRegistry()

	registry.Register("append", OperatorInfo{Phase: MergePhase})
	registry.Register("param", OperatorInfo{Phase: ParamPhase})
	registry.Register("grab", OperatorInfo{Phase: EvalPhase})
	registry.Register("prepend", OperatorInfo{Phase: MergePhase})

	mergeOps := registry.ListByPhase(MergePhase)
	expected := []string{"append", "prepend"}
	sort.Strings(expected)
	if len(mergeOps) != len(expected) {
		t.Fatalf("ListByPhase(MergePhase) returned %d items, want %d", len(mergeOps), len(expected))
	}
	for i, name := range expected {
		if mergeOps[i] != name {
			t.Errorf("ListByPhase(MergePhase)[%d] = %q, want %q", i, mergeOps[i], name)
		}
	}

	paramOps := registry.ListByPhase(ParamPhase)
	if len(paramOps) != 1 || paramOps[0] != "param" {
		t.Errorf("ListByPhase(ParamPhase) = %v, want [param]", paramOps)
	}
}

// TestOperatorRegistryCount tests the Count method.
func TestOperatorRegistryCount(t *testing.T) {
	registry := NewOperatorRegistry()

	if count := registry.Count(); count != 0 {
		t.Errorf("empty registry Count() = %d, want 0", count)
	}

	registry.Register("one", OperatorInfo{})
	registry.Register("two", OperatorInfo{})
	registry.Register("three", OperatorInfo{})

	if count := registry.Count(); count != 3 {
		t.Errorf("Count() = %d, want 3", count)
	}
}

// TestOperatorRegistryConcurrency tests thread-safety of the registry.
func TestOperatorRegistryConcurrency(t *testing.T) {
	registry := NewOperatorRegistry()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := string(rune('a' + (n % 26)))
			registry.Register(name, OperatorInfo{
				MinArgs: n,
			})
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := string(rune('a' + (n % 26)))
			registry.Lookup(name)
			registry.List()
			registry.Count()
		}(i)
	}

	wg.Wait()

	// Verify registry is in consistent state
	if count := registry.Count(); count > 26 || count == 0 {
		t.Errorf("unexpected Count() = %d after concurrent access", count)
	}
}

// TestGlobalRegistryFunctions tests the global registry helper functions.
func TestGlobalRegistryFunctions(t *testing.T) {
	// These test the globally registered operators from init()

	// Test LookupOperator
	info, ok := LookupOperator("grab")
	if !ok {
		t.Fatal("expected grab to be registered")
	}
	if info.Category != CategoryData {
		t.Errorf("grab.Category = %v, want CategoryData", info.Category)
	}

	// Test IsOperatorName
	if !IsOperatorName("grab") {
		t.Error("IsOperatorName(grab) = false, want true")
	}
	if IsOperatorName("nonexistent_op") {
		t.Error("IsOperatorName(nonexistent_op) = true, want false")
	}

	// Test OperatorPrecedence
	if prec := OperatorPrecedence("+"); prec != PrecedenceAdditive {
		t.Errorf("OperatorPrecedence(+) = %v, want PrecedenceAdditive", prec)
	}
	if prec := OperatorPrecedence("*"); prec != PrecedenceMultiplicative {
		t.Errorf("OperatorPrecedence(*) = %v, want PrecedenceMultiplicative", prec)
	}
	if prec := OperatorPrecedence("nonexistent"); prec != PrecedenceLowest {
		t.Errorf("OperatorPrecedence(nonexistent) = %v, want PrecedenceLowest", prec)
	}

	// Test IsUnaryOperator
	if !IsUnaryOperator("!") {
		t.Error("IsUnaryOperator(!) = false, want true")
	}
	if !IsUnaryOperator("-") {
		t.Error("IsUnaryOperator(-) = false, want true")
	}
	if IsUnaryOperator("+") {
		t.Error("IsUnaryOperator(+) = true, want false")
	}

	// Test IsBinaryOperator
	if !IsBinaryOperator("+") {
		t.Error("IsBinaryOperator(+) = false, want true")
	}
	if !IsBinaryOperator("-") {
		t.Error("IsBinaryOperator(-) = false, want true")
	}
	if !IsBinaryOperator("==") {
		t.Error("IsBinaryOperator(==) = false, want true")
	}
	if IsBinaryOperator("!") {
		t.Error("IsBinaryOperator(!) = true, want false")
	}
}

// TestGetOperatorAssociativity tests the associativity helper function.
func TestGetOperatorAssociativity(t *testing.T) {
	tests := []struct {
		op       string
		expected Associativity
	}{
		{"+", LeftAssociative},
		{"-", LeftAssociative},
		{"*", LeftAssociative},
		{"/", LeftAssociative},
		{"!", RightAssociative},
		{"?:", RightAssociative},
		{"<", NonAssociative},
		{">", NonAssociative},
		{"nonexistent", LeftAssociative}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			if got := GetOperatorAssociativity(tt.op); got != tt.expected {
				t.Errorf("GetOperatorAssociativity(%q) = %v, want %v", tt.op, got, tt.expected)
			}
		})
	}
}

// TestValidateOperatorArgs tests argument validation.
func TestValidateOperatorArgs(t *testing.T) {
	tests := []struct {
		op       string
		count    int
		expected bool
	}{
		// grab: min=1, max=-1 (variadic)
		{"grab", 0, false},
		{"grab", 1, true},
		{"grab", 5, true},
		{"grab", 100, true},

		// concat: min=1, max=-1 (variadic)
		{"concat", 0, false},
		{"concat", 1, true},
		{"concat", 10, true},

		// +: min=2, max=2
		{"+", 0, false},
		{"+", 1, false},
		{"+", 2, true},
		{"+", 3, false},

		// !: min=1, max=1
		{"!", 0, false},
		{"!", 1, true},
		{"!", 2, false},

		// null: min=0, max=0
		{"null", 0, true},
		{"null", 1, false},

		// nonexistent
		{"nonexistent", 0, false},
		{"nonexistent", 5, false},
	}

	for _, tt := range tests {
		name := tt.op + "_" + strconv.Itoa(tt.count)
		t.Run(name, func(t *testing.T) {
			if got := ValidateOperatorArgs(tt.op, tt.count); got != tt.expected {
				t.Errorf("ValidateOperatorArgs(%q, %d) = %v, want %v",
					tt.op, tt.count, got, tt.expected)
			}
		})
	}
}

// TestAllDataOperatorsRegistered tests that all data operators are registered.
func TestAllDataOperatorsRegistered(t *testing.T) {
	expected := []string{
		"grab", "static", "param", "inject", "prune", "sort", "uniq",
		"reverse", "defer", "stringify", "parse", "base64", "base64-decode",
	}

	for _, name := range expected {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("data operator %q not registered", name)
			continue
		}
		if info.Category != CategoryData {
			t.Errorf("%q.Category = %v, want CategoryData", name, info.Category)
		}
	}
}

// TestAllArithmeticOperatorsRegistered tests that all arithmetic operators are registered.
func TestAllArithmeticOperatorsRegistered(t *testing.T) {
	expected := []string{"+", "-", "*", "/", "%", "calc"}

	for _, name := range expected {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("arithmetic operator %q not registered", name)
			continue
		}
		if info.Category != CategoryArithmetic {
			t.Errorf("%q.Category = %v, want CategoryArithmetic", name, info.Category)
		}
	}

	// Test alias
	info, ok := LookupOperator("mod")
	if !ok {
		t.Error("alias 'mod' not registered")
	} else if info.Name != "%" {
		t.Errorf("mod resolves to %q, want %%", info.Name)
	}
}

// TestAllStringOperatorsRegistered tests that all string operators are registered.
func TestAllStringOperatorsRegistered(t *testing.T) {
	expected := []string{
		"concat", "join", "split", "substr", "replace", "trim", "upper", "lower",
	}

	for _, name := range expected {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("string operator %q not registered", name)
			continue
		}
		if info.Category != CategoryString {
			t.Errorf("%q.Category = %v, want CategoryString", name, info.Category)
		}
	}
}

// TestAllLogicOperatorsRegistered tests that all logic operators are registered.
func TestAllLogicOperatorsRegistered(t *testing.T) {
	expected := []string{"||", "&&", "!", "?:"}

	for _, name := range expected {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("logic operator %q not registered", name)
			continue
		}
		if info.Category != CategoryLogic {
			t.Errorf("%q.Category = %v, want CategoryLogic", name, info.Category)
		}
	}

	// Test aliases
	aliases := map[string]string{
		"or":      "||",
		"and":     "&&",
		"not":     "!",
		"ternary": "?:",
	}
	for alias, canonical := range aliases {
		info, ok := LookupOperator(alias)
		if !ok {
			t.Errorf("alias %q not registered", alias)
		} else if info.Name != canonical {
			t.Errorf("%q resolves to %q, want %q", alias, info.Name, canonical)
		}
	}
}

// TestAllComparisonOperatorsRegistered tests that all comparison operators are registered.
func TestAllComparisonOperatorsRegistered(t *testing.T) {
	expected := []string{"==", "!=", "<", "<=", ">", ">="}

	for _, name := range expected {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("comparison operator %q not registered", name)
			continue
		}
		if info.Category != CategoryComparison {
			t.Errorf("%q.Category = %v, want CategoryComparison", name, info.Category)
		}
	}

	// Test aliases
	aliases := map[string]string{
		"eq": "==",
		"ne": "!=",
		"lt": "<",
		"le": "<=",
		"gt": ">",
		"ge": ">=",
	}
	for alias, canonical := range aliases {
		info, ok := LookupOperator(alias)
		if !ok {
			t.Errorf("alias %q not registered", alias)
		} else if info.Name != canonical {
			t.Errorf("%q resolves to %q, want %q", alias, info.Name, canonical)
		}
	}
}

// TestAllArrayOperatorsRegistered tests that all array operators are registered.
func TestAllArrayOperatorsRegistered(t *testing.T) {
	expected := []string{
		"append", "prepend", "insert", "delete", "index", "length",
		"first", "last", "flatten", "inline", "merge", "shuffle",
		"cartesian-product",
	}

	for _, name := range expected {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("array operator %q not registered", name)
			continue
		}
		if info.Category != CategoryArray {
			t.Errorf("%q.Category = %v, want CategoryArray", name, info.Category)
		}
	}

	// Test alias
	info, ok := LookupOperator("len")
	if !ok {
		t.Error("alias 'len' not registered")
	} else if info.Name != "length" {
		t.Errorf("len resolves to %q, want length", info.Name)
	}
}

// TestAllControlOperatorsRegistered tests that all control operators are registered.
func TestAllControlOperatorsRegistered(t *testing.T) {
	expected := []string{
		"if", "elif", "else", "fi", "for", "while", "done",
		"case", "when", "default", "esac", "in", "range",
	}

	for _, name := range expected {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("control operator %q not registered", name)
			continue
		}
		if info.Category != CategoryControl {
			t.Errorf("%q.Category = %v, want CategoryControl", name, info.Category)
		}
	}
}

// TestAllExternalOperatorsRegistered tests that all external operators are registered.
func TestAllExternalOperatorsRegistered(t *testing.T) {
	expected := []string{
		"vault", "awsparam", "awssecret", "nats", "file", "load", "env",
	}

	for _, name := range expected {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("external operator %q not registered", name)
			continue
		}
		if info.Category != CategoryExternal {
			t.Errorf("%q.Category = %v, want CategoryExternal", name, info.Category)
		}
	}

	// Test aliases
	aliases := map[string]string{
		"awssm":      "awsparam",
		"awssecrets": "awssecret",
	}
	for alias, canonical := range aliases {
		info, ok := LookupOperator(alias)
		if !ok {
			t.Errorf("alias %q not registered", alias)
		} else if info.Name != canonical {
			t.Errorf("%q resolves to %q, want %q", alias, info.Name, canonical)
		}
	}
}

// TestAllTypeOperatorsRegistered tests that all type operators are registered.
func TestAllTypeOperatorsRegistered(t *testing.T) {
	expected := []string{"type", "empty", "null", "keys", "values"}

	for _, name := range expected {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("type operator %q not registered", name)
			continue
		}
		if info.Category != CategoryType {
			t.Errorf("%q.Category = %v, want CategoryType", name, info.Category)
		}
	}

	// Test alias
	info, ok := LookupOperator("nil")
	if !ok {
		t.Error("alias 'nil' not registered")
	} else if info.Name != "null" {
		t.Errorf("nil resolves to %q, want null", info.Name)
	}
}

// TestAllIPOperatorsRegistered tests that all IP operators are registered.
func TestAllIPOperatorsRegistered(t *testing.T) {
	expected := []string{"ips", "static_ips"}

	for _, name := range expected {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("IP operator %q not registered", name)
			continue
		}
		if info.Category != CategoryIP {
			t.Errorf("%q.Category = %v, want CategoryIP", name, info.Category)
		}
	}
}

// TestPrecedenceOrdering tests that precedence levels are correctly ordered.
func TestPrecedenceOrdering(t *testing.T) {
	// Higher precedence operators should have higher values
	precedences := []struct {
		name string
		prec Precedence
	}{
		{"?:", PrecedenceTernary},
		{"||", PrecedenceOr},
		{"&&", PrecedenceAnd},
		{"==", PrecedenceEquality},
		{"<", PrecedenceComparison},
		{"+", PrecedenceAdditive},
		{"*", PrecedenceMultiplicative},
		{"!", PrecedenceUnary},
		{"grab", PrecedenceCall},
	}

	// Verify ordering
	for i := 1; i < len(precedences); i++ {
		prev := precedences[i-1]
		curr := precedences[i]

		if OperatorPrecedence(prev.name) >= OperatorPrecedence(curr.name) {
			t.Errorf("Precedence(%q) = %d should be less than Precedence(%q) = %d",
				prev.name, OperatorPrecedence(prev.name),
				curr.name, OperatorPrecedence(curr.name))
		}
	}
}

// TestMergePhaseOperators tests that merge-phase operators are correctly identified.
func TestMergePhaseOperators(t *testing.T) {
	mergeOps := []string{"append", "prepend", "insert", "delete", "inline", "merge", "defer"}

	for _, name := range mergeOps {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("operator %q not registered", name)
			continue
		}
		if info.Phase != MergePhase {
			t.Errorf("%q.Phase = %v, want MergePhase", name, info.Phase)
		}
	}
}

// TestParamPhaseOperators tests that param-phase operators are correctly identified.
func TestParamPhaseOperators(t *testing.T) {
	info, ok := LookupOperator("param")
	if !ok {
		t.Fatal("param operator not registered")
	}
	if info.Phase != ParamPhase {
		t.Errorf("param.Phase = %v, want ParamPhase", info.Phase)
	}
}

// TestOperatorCount tests that we have at least 50 operators registered.
func TestOperatorCount(t *testing.T) {
	count := GetGlobalRegistry().Count()
	if count < 50 {
		t.Errorf("Expected at least 50 operators, got %d", count)
	}
	t.Logf("Total operators registered: %d", count)
}

// TestListOperatorsSorted tests that ListOperators returns a sorted list.
func TestListOperatorsSorted(t *testing.T) {
	ops := ListOperators()

	for i := 1; i < len(ops); i++ {
		if ops[i-1] > ops[i] {
			t.Errorf("ListOperators() not sorted: %q > %q at positions %d, %d",
				ops[i-1], ops[i], i-1, i)
		}
	}
}

// TestVariadicOperators tests that variadic operators accept unlimited args.
func TestVariadicOperators(t *testing.T) {
	variadicOps := []string{"grab", "concat", "vault", "append", "prepend", "when", "cartesian-product", "static_ips"}

	for _, name := range variadicOps {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("variadic operator %q not registered", name)
			continue
		}
		if info.MaxArgs != -1 {
			t.Errorf("%q.MaxArgs = %d, want -1 (variadic)", name, info.MaxArgs)
		}
	}
}

// TestBinaryOperatorPairs tests that binary operators have correct min/max args.
func TestBinaryOperatorPairs(t *testing.T) {
	binaryOps := []string{"+", "*", "/", "==", "!=", "<", "<=", ">", ">=", "&&", "||"}

	for _, name := range binaryOps {
		info, ok := LookupOperator(name)
		if !ok {
			t.Errorf("binary operator %q not registered", name)
			continue
		}
		if info.MinArgs < 2 {
			t.Errorf("%q.MinArgs = %d, want >= 2", name, info.MinArgs)
		}
		if info.MaxArgs != 2 && info.MaxArgs != -1 {
			t.Errorf("%q.MaxArgs = %d, want 2 or -1", name, info.MaxArgs)
		}
	}
}

// TestMinusOperatorDual tests that minus is both unary and binary.
func TestMinusOperatorDual(t *testing.T) {
	info, ok := LookupOperator("-")
	if !ok {
		t.Fatal("minus operator not registered")
	}

	if !info.IsUnary {
		t.Error("minus should be unary")
	}
	if !info.IsBinary {
		t.Error("minus should be binary")
	}
	if info.MinArgs != 1 {
		t.Errorf("minus.MinArgs = %d, want 1", info.MinArgs)
	}
	if info.MaxArgs != 2 {
		t.Errorf("minus.MaxArgs = %d, want 2", info.MaxArgs)
	}
}

// TestGetGlobalRegistry tests the GetGlobalRegistry function.
func TestGetGlobalRegistry(t *testing.T) {
	reg := GetGlobalRegistry()
	if reg == nil {
		t.Fatal("GetGlobalRegistry() returned nil")
	}

	// Verify it's the same registry used by global functions
	info1, ok1 := reg.Lookup("grab")
	info2, ok2 := LookupOperator("grab")

	if ok1 != ok2 {
		t.Error("global registry mismatch")
	}
	if info1.Name != info2.Name {
		t.Error("global registry returned different results")
	}
}

// TestListOperatorsNonEmpty tests that the global registry isn't empty.
func TestListOperatorsNonEmpty(t *testing.T) {
	ops := ListOperators()
	if len(ops) == 0 {
		t.Error("ListOperators() returned empty list")
	}
}

// TestListByCategory tests listing operators by each category.
func TestListByCategory(t *testing.T) {
	categories := []OperatorCategory{
		CategoryData,
		CategoryArithmetic,
		CategoryString,
		CategoryLogic,
		CategoryComparison,
		CategoryArray,
		CategoryControl,
		CategoryExternal,
		CategoryType,
		CategoryIP,
	}

	for _, cat := range categories {
		ops := ListOperatorsByCategory(cat)
		if len(ops) == 0 {
			t.Errorf("ListOperatorsByCategory(%v) returned empty list", cat)
		}
		t.Logf("%v: %d operators", cat.String(), len(ops))
	}
}

// BenchmarkOperatorLookup benchmarks the lookup performance.
func BenchmarkOperatorLookup(b *testing.B) {
	for i := 0; i < b.N; i++ {
		LookupOperator("grab")
		LookupOperator("concat")
		LookupOperator("+")
		LookupOperator("vault")
	}
}

// BenchmarkOperatorLookupAlias benchmarks alias lookup performance.
func BenchmarkOperatorLookupAlias(b *testing.B) {
	for i := 0; i < b.N; i++ {
		LookupOperator("mod")
		LookupOperator("or")
		LookupOperator("eq")
		LookupOperator("len")
	}
}

// BenchmarkListOperators benchmarks listing all operators.
func BenchmarkListOperators(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ListOperators()
	}
}

// BenchmarkListByCategory benchmarks listing by category.
func BenchmarkListByCategory(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ListOperatorsByCategory(CategoryData)
		ListOperatorsByCategory(CategoryArithmetic)
		ListOperatorsByCategory(CategoryExternal)
	}
}
