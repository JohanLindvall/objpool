package objpool

import "sync"

// Pool is a typed wrapper over sync.Pool: Get returns a ready *T — never nil,
// never needing a type assertion — and Put offers one back.
//
// It keeps sync.Pool's semantics, including the one that matters most: Put is a
// hint, not storage. The runtime may drop a pooled element at any time, and
// drops every idle one within two garbage collections, so nothing whose loss
// would matter may live only in a Pool; see FreeList for elements too costly to
// lose.
//
// A Pool is safe for concurrent use; the elements it hands out are not shared.
// Create one with New: the zero value is not usable. A Pool must not be copied
// after first use.
type Pool[T any] struct {
	factory factory[T]
	pool    sync.Pool
}

// New returns a Pool that builds elements with newFn and rewinds recycled ones
// with resetFn.
//
// newFn runs whenever Get finds the pool empty, on the caller's goroutine, and
// may capture whatever construction needs. It must return a ready, non-nil
// element.
//
// resetFn, when non-nil, rewinds a recycled element before Get hands it out,
// which is what lets Get promise a clean element instead of leaving each call
// site to remember. It runs in Get rather than in Put deliberately: a reset that
// fails can then be answered with a fresh element, whereas in Put there is no
// caller left to tell. It never runs on a freshly built element — newFn already
// returns a ready one — so required setup belongs in newFn, not resetFn. Pass
// nil when there is nothing to rewind, or when rewinding needs an argument only
// the call site has: a pooled gzip.Writer, say, is reset onto its destination
// by the caller after Get.
//
// A resetFn that returns an error declares the element unfit to reuse: Get
// drops it and builds a replacement, so the error costs one rebuild and never
// reaches the caller. That path exists so that a reset which can fail never
// hands back a half-rewound element.
//
// Get's never-nil guarantee rests on newFn, so both ways of breaking it panic
// early, with a message naming the cause: New panics if newFn is nil — at
// construction, typically program start, rather than on the first miss under
// load — and Get panics if newFn returns nil, rather than handing back a nil
// that dereferences in unrelated code. Neither check touches a pool hit.
func New[T any](newFn func() *T, resetFn func(*T) error) *Pool[T] {
	return &Pool[T]{factory: newFactory("objpool.New", newFn, resetFn)}
}

// Get returns a recycled element rewound by resetFn, or a freshly built one
// when the pool has none — never nil. The caller owns it until handing it back
// with Put.
func (p *Pool[T]) Get() *T {
	// sync.Pool.New stays unset, so a miss arrives here as nil and reuse builds
	// a fresh element without running the reset over it.
	v, _ := p.pool.Get().(*T)
	return p.factory.reuse(v)
}

// Put offers v back for reuse; a nil v is ignored. Rewinding is Get's job (see
// New), so v keeps its contents until it is next drawn or the runtime drops it.
func (p *Pool[T]) Put(v *T) {
	if v != nil {
		p.pool.Put(v)
	}
}
