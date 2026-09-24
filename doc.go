// Package objpool provides typed object pools: Pool, a statically typed
// wrapper over sync.Pool, and FreeList, its GC-proof counterpart for few,
// large elements. A third piece, RatchetTrim, decides when a pooled element
// holds far more memory than its work uses, and should be dropped rather than
// reused.
//
// The untyped sync.Pool leaves two things to every call site: the type assertion,
// and what to do when the pool is empty — and call sites that each handle those
// on their own diverge. Folding the element type into the pool makes Get return a
// ready *T, so a mis-asserted type is a compile error and the empty-pool path
// cannot be forgotten.
//
// # Choosing a pool
//
// Pool suits many small, cheap elements: like sync.Pool, it may drop idle
// elements at any time, so it never pins memory it no longer needs. FreeList
// suits few, large, expensive ones: it never drops what it is given, so reuse
// survives garbage collection, and its size is bounded by the peak number of
// elements checked out at once. It does not bound the memory each element
// retains; RatchetTrim decides when an element that a past burst inflated
// should be dropped instead of reused.
//
// # Element lifecycle
//
// Both pools take the same two functions, whose full contract New describes:
//
//   - newFn builds an element whenever Get has none to reuse. It must return a
//     ready, non-nil element.
//   - resetFn, when non-nil, rewinds a recycled element before Get returns it.
//     It never runs on a freshly built element, and an error from it drops the
//     element in favor of a fresh build.
//
// Get therefore never returns nil. Put ignores nil and does no work beyond
// parking the element; the caller must not use an element after putting it back.
package objpool
