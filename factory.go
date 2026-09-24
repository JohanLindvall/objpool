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

// rewind runs resetFn, if any, over v, a recycled element, and reports whether
// v may be handed out: false when the reset declares it unfit to reuse. It is
// the whole of Get's hit path, and small enough to inline there.
func (f *factory[T]) rewind(v *T) bool {
	return f.resetFn == nil || f.resetFn(v) == nil
}

// build returns a freshly built element — never nil, and never reset. It is
// Get's miss path, kept apart from rewind so that rewind stays inlinable.
func (f *factory[T]) build() *T {
	v := f.newFn()
	if v == nil {
		panic(f.ctor + ": newFn returned nil")
	}
	return v
}
