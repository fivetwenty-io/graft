package tree

import (
	"errors"
	"testing"
)

// testData builds a common map[string]interface{} structure for resolver tests.
func testData() map[string]interface{} {
	return map[string]interface{}{
		"root": map[string]interface{}{
			"child": map[string]interface{}{
				"value":  "test",
				"number": 42,
			},
			"list": []interface{}{
				"item0",
				map[string]interface{}{
					"name":  "item1",
					"value": "named_item",
				},
				"item2",
			},
		},
		"simple":  "simple_value",
		"boolval": true,
		"numval":  float64(3.14),
		"nested": map[string]interface{}{
			"map": map[string]interface{}{
				"key": "val",
			},
			"arr": []interface{}{"a", "b"},
		},
	}
}

// --- Resolve ---

func TestResolveSimplePath(t *testing.T) {
	data := testData()
	c, err := ParseCursor("simple")
	if err != nil {
		t.Fatalf("ParseCursor: %v", err)
	}
	result, err := c.Resolve(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "simple_value" {
		t.Errorf("got %v, want %v", result, "simple_value")
	}
}

func TestResolveNestedPath(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.child.value")
	result, err := c.Resolve(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "test" {
		t.Errorf("got %v, want %v", result, "test")
	}
}

func TestResolveListByIndex(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.list.0")
	result, err := c.Resolve(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "item0" {
		t.Errorf("got %v, want %v", result, "item0")
	}
}

func TestResolveListByNameField(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.list.item1")
	result, err := c.Resolve(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}
	if m["name"] != "item1" {
		t.Errorf("got name=%v, want item1", m["name"])
	}
}

func TestResolveNotFound(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.nonexistent")
	_, err := c.Resolve(data)
	if err == nil {
		t.Fatal("expected NotFoundError, got nil")
	}
	var notFound NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestResolveTypeMismatch(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("simple.child")
	_, err := c.Resolve(data)
	if err == nil {
		t.Fatal("expected TypeMismatchError, got nil")
	}
	var typeMismatch TypeMismatchError
	if !errors.As(err, &typeMismatch) {
		t.Errorf("expected TypeMismatchError, got %T: %v", err, err)
	}
}

func TestResolveListOutOfRange(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.list.99")
	_, err := c.Resolve(data)
	if err == nil {
		t.Fatal("expected NotFoundError, got nil")
	}
	var notFound NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestResolveListItemNotFound(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.list.nosuchname")
	_, err := c.Resolve(data)
	if err == nil {
		t.Fatal("expected NotFoundError, got nil")
	}
	var notFound NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

// --- Canonical ---

func TestCanonicalSimplePath(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("simple")
	canon, err := c.Canonical(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canon.String() != "simple" {
		t.Errorf("got %v, want simple", canon.String())
	}
}

func TestCanonicalNestedPath(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.child.value")
	canon, err := c.Canonical(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canon.String() != "root.child.value" {
		t.Errorf("got %v, want root.child.value", canon.String())
	}
}

func TestCanonicalListByIndex(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.list.0")
	canon, err := c.Canonical(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canon.String() != "root.list.0" {
		t.Errorf("got %v, want root.list.0", canon.String())
	}
}

func TestCanonicalListByNameToIndex(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.list.item1")
	canon, err := c.Canonical(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// item1 is at index 1, canonical form uses numeric index
	if canon.String() != "root.list.1" {
		t.Errorf("got %v, want root.list.1", canon.String())
	}
}

func TestCanonicalNotFound(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.nonexistent")
	_, err := c.Canonical(data)
	if err == nil {
		t.Fatal("expected NotFoundError, got nil")
	}
	var notFound NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestCanonicalTypeMismatch(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("simple.child")
	_, err := c.Canonical(data)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- ResolveString ---

func TestResolveStringValue(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("simple")
	s, err := c.ResolveString(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "simple_value" {
		t.Errorf("got %q, want simple_value", s)
	}
}

func TestResolveStringFromInt(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("root.child.number")
	s, err := c.ResolveString(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "42" {
		t.Errorf("got %q, want 42", s)
	}
}

func TestResolveStringTypeMismatch(t *testing.T) {
	data := testData()
	c, _ := ParseCursor("boolval")
	_, err := c.ResolveString(data)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// --- Find helpers ---

func TestFindWithStringMap(t *testing.T) {
	data := testData()
	result, err := Find(data, "root.child.value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "test" {
		t.Errorf("got %v, want test", result)
	}
}

func TestFindStringWithStringMap(t *testing.T) {
	data := testData()
	s, err := FindString(data, "simple")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "simple_value" {
		t.Errorf("got %q, want simple_value", s)
	}
}

func TestFindStringTypeMismatch(t *testing.T) {
	data := testData()
	_, err := FindString(data, "root.child.number")
	if err == nil {
		t.Fatal("expected error for non-string value, got nil")
	}
}

func TestFindNumWithStringMap(t *testing.T) {
	data := testData()
	n, err := FindNum(data, "numval")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Float64() != 3.14 {
		t.Errorf("got %v, want 3.14", n.Float64())
	}
}

func TestFindNumTypeMismatch(t *testing.T) {
	data := testData()
	_, err := FindNum(data, "simple")
	if err == nil {
		t.Fatal("expected error for non-numeric value, got nil")
	}
}

func TestFindBoolWithStringMap(t *testing.T) {
	data := testData()
	b, err := FindBool(data, "boolval")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !b {
		t.Error("expected true, got false")
	}
}

func TestFindBoolTypeMismatch(t *testing.T) {
	data := testData()
	_, err := FindBool(data, "simple")
	if err == nil {
		t.Fatal("expected error for non-bool value, got nil")
	}
}

func TestFindMapWithStringMap(t *testing.T) {
	data := testData()
	m, err := FindMap(data, "nested.map")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["key"] != "val" {
		t.Errorf("got key=%v, want val", m["key"])
	}
}

func TestFindMapTypeMismatch(t *testing.T) {
	data := testData()
	_, err := FindMap(data, "simple")
	if err == nil {
		t.Fatal("expected error for non-map value, got nil")
	}
}

func TestFindArrayWithStringMap(t *testing.T) {
	data := testData()
	arr, err := FindArray(data, "nested.arr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(arr) != 2 {
		t.Errorf("got len=%d, want 2", len(arr))
	}
	if arr[0] != "a" {
		t.Errorf("got arr[0]=%v, want a", arr[0])
	}
}

func TestFindArrayTypeMismatch(t *testing.T) {
	data := testData()
	_, err := FindArray(data, "simple")
	if err == nil {
		t.Fatal("expected error for non-array value, got nil")
	}
}

func TestFindNotFound(t *testing.T) {
	data := testData()
	_, err := Find(data, "does.not.exist")
	if err == nil {
		t.Fatal("expected NotFoundError, got nil")
	}
	var notFound NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}
