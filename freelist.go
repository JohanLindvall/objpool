package objpool

import "sync"

// FreeList is the GC-proof counterpart of Pool, for elements that are few,
// large, and expensive to rebuild — an encoder whose buffers and compression
// state have grown to megabytes, say. A Pool silently defeats reuse for such
// elements once garbage collections outpace their use cycle: at a high
// allocation rate the collector runs several times per cycle, so the Pool
// rebuilds the element nearly every time, and the rebuilds' allocations feed
// the very collection rate that evicts it.
//
// A FreeList never evicts, yet stays bounded without a cap: Get builds only
// when the list is empty, so the number of elements never exceeds the peak
// number checked out at once — for a fixed set of workers, the number of
// workers, whose memory the process must afford at peak anyway. The price is
// that memory stays at that peak: elements parked beyond current demand are
// kept, not released.
//
// Nor does the list bound the memory each element retains. An element whose
// footprint can ratchet upward (a burst grows its buffers; quieter traffic
// never shrinks them) needs a trim policy at the caller, who drops the element
// instead of putting it back — the list keeps whatever it is given, forever.
// RatchetTrim is such a policy.
//
// A FreeList is safe for concurrent use; the elements it hands out are not
// shared. Create one with NewFreeList: the zero value is not usable. A FreeList
// must not be copied after first use.
type FreeList[T any] struct {
	factory factory[T]
	mu      sync.Mutex
	free    []*T // parked elements, the most recently parked last
	maxIdle int
}

// NewFreeList returns a FreeList that builds elements with newFn, rewinds
// parked ones with resetFn, and parks at most maxIdle idle elements (maxIdle
// <= 0: no limit).
//
// newFn and resetFn follow New's contract exactly, panics included. Reuse is
// the whole point of a FreeList, so a reset must keep the capacity the list
// exists to keep warm: truncating a buffer to length zero is right; replacing
// it with a new one defeats the list.
//
// maxIdle is defense in depth on top of the structural bound (see FreeList): a
// Put that finds maxIdle elements already parked drops its element to the
// garbage collector. Set it comfortably above the peak number of elements
// checked out at once, because a cap below that quietly reintroduces the
// rebuild churn the list exists to prevent — visible as newFn calls that keep
// coming after warm-up. It never limits Get.
func NewFreeList[T any](maxIdle int, newFn func() *T, resetFn func(*T) error) *FreeList[T] {
	return &FreeList[T]{factory: newFactory("objpool.NewFreeList", newFn, resetFn), maxIdle: maxIdle}
}

// Get returns a parked element rewound by resetFn, or a freshly built one when
// the list is empty — never nil. Unlike Pool.Get, it hands back a parked
// element whenever one remains: the most recently parked, replaced by a fresh
// build only if its reset reports it unfit.
func (l *FreeList[T]) Get() *T {
	var v *T
	l.mu.Lock()
	if n := len(l.free); n > 0 {
		v = l.free[n-1]
		l.free[n-1] = nil // so the backing array does not pin an element its holder drops
		l.free = l.free[:n-1]
	}
	l.mu.Unlock()
	// Reset or build outside the lock: v belongs to this caller once it is off
	// the list, and holding the mutex across caller-supplied code would
	// serialize every Get behind it. The ifs stay nested, as in Pool.Get.
	if v != nil {
		if l.factory.rewind(v) {
			return v
		}
	}
	return l.factory.build()
}

// Put parks v for reuse, or drops it when maxIdle elements are already parked;
// a nil v is ignored. Rewinding is Get's job (see NewFreeList), so v keeps its
// contents until it is next drawn. Putting the same element twice hands it out
// twice: exclusivity is the caller's to keep, as with Pool.
func (l *FreeList[T]) Put(v *T) {
	if v == nil {
		return
	}
	l.mu.Lock()
	if l.maxIdle <= 0 || len(l.free) < l.maxIdle {
		l.free = append(l.free, v)
	}
	l.mu.Unlock()
}
