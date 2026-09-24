package objpool

// factory is the element contract Pool and FreeList share (see New): newFn
// builds, resetFn rewinds, and ctor names the constructor that received them,
// for panic messages.
type factory[T any] struct {
	newFn   func() *T
	resetFn func(*T) error
	ctor    string
}

// newFactory panics on a nil newFn here, at construction, rather than on the
// first miss under load.
func newFactory[T any](ctor string, newFn func() *T, resetFn func(*T) error) factory[T] {
	if newFn == nil {
		panic(ctor + ": newFn must not be nil")
	}
	return factory[T]{newFn: newFn, resetFn: resetFn, ctor: ctor}
}

// reuse returns v rewound for reuse, or a freshly built element when v is nil
// (a miss) or its reset reports it unfit — never nil. A fresh element is never
// reset.
func (f *factory[T]) reuse(v *T) *T {
	if v != nil && (f.resetFn == nil || f.resetFn(v) == nil) {
		return v
	}
	v = f.newFn()
	if v == nil {
		panic(f.ctor + ": newFn returned nil")
	}
	return v
}
