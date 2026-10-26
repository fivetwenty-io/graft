package value

import "sync"

// spare lets the values that a loop grows one step at a time share one
// backing array, so appending to an array or adding a key to an object
// costs amortized constant time rather than a copy of the container. Every
// value on a store holds a prefix of it. Only a value that ends where the
// claimed slots end may claim the room after it, and a claimed slot is
// never written again, so no value ever sees a change to what it holds.
type spare struct {
	mu   sync.RWMutex
	used int // slots claimed, guarded by mu

	// An object's store indexes its keys. Every value on the store holds
	// at least its first base entries, and index maps each key among them
	// to its last position. added maps each key claimed since to its
	// position, under mu. A claimed key was absent from everything before
	// it, so it occurs once.
	base  int
	once  sync.Once
	index map[string]int
	added map[string]int
}

// room is the capacity that a container growing to n gets when it has to
// copy, and 0 below the size where keeping spare room pays.
func room(n int) int {
	if n < indexThreshold {
		return 0
	}
	return n + n/2
}

// find returns the position of key among entries, which are a value's own
// entries on this store.
func (s *spare) find(entries []Entry, key string) (int, bool) {
	s.once.Do(func() { s.index = buildIndex(entries[:s.base]) })
	if i, ok := s.index[key]; ok {
		return i, true
	}
	s.mu.RLock()
	i, ok := s.added[key]
	s.mu.RUnlock()
	return i, ok && i < len(entries)
}

// find returns the position of the last entry of an object whose key
// formats to key.
func (v *Value) find(key string) (int, bool) {
	if v.grow != nil {
		return v.grow.find(v.obj.entries, key)
	}
	return v.obj.find(key)
}

// Append returns a copy of an array with elems added at the end. Meta is
// carried.
// It panics when v is not an array, which is part of the contract.
func (v *Value) Append(elems ...*Value) *Value {
	v.mustBe(KindArray, "Append")
	if len(elems) == 0 {
		return v
	}
	out := v.copyShell()
	n, total := len(v.arr), len(v.arr)+len(elems)
	if s := v.grow; s != nil {
		s.mu.Lock()
		if s.used == n && total <= cap(v.arr) {
			out.arr = append(v.arr, elems...)
			out.grow = s
			s.used = total
			s.mu.Unlock()
			return out
		}
		s.mu.Unlock()
	}
	arr := make([]*Value, total, max(total, room(total)))
	copy(arr, v.arr)
	copy(arr[n:], elems)
	out.arr = arr
	if cap(arr) > total {
		out.grow = &spare{used: total}
	}
	return out
}

// add returns a copy of an object with e appended, where key is e's
// formatted key and v holds no entry for it. Meta is carried.
func (v *Value) add(e Entry, key string) *Value {
	out := v.copyShell()
	n := len(v.obj.entries)
	if s := v.grow; s != nil {
		s.mu.Lock()
		if s.used == n && n < cap(v.obj.entries) {
			if s.added == nil {
				s.added = map[string]int{}
			}
			s.added[key] = n
			s.used = n + 1
			s.mu.Unlock()
			out.obj = &object{entries: append(v.obj.entries, e)}
			out.grow = s
			return out
		}
		s.mu.Unlock()
	}
	entries := make([]Entry, n+1, max(n+1, room(n+1)))
	copy(entries, v.obj.entries)
	entries[n] = e
	out.obj = newObject(entries)
	if cap(entries) > n+1 {
		out.grow = &spare{used: n + 1, base: n + 1}
	}
	return out
}
