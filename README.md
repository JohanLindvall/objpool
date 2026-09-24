# objpool

Typed object pools for Go.

- **`Pool[T]`** is a statically typed wrapper over `sync.Pool`: `Get` returns a
  ready `*T`, never nil, with no type assertion.
- **`FreeList[T]`** is its GC-proof counterpart, for few, large elements too
  costly to lose to a garbage collection.
- **`RatchetTrim`** is a drop policy for pooled elements holding on to memory
  that a past burst made them grow.

```go
buffers := objpool.New(
	func() *bytes.Buffer { return new(bytes.Buffer) },     // build on a miss
	func(b *bytes.Buffer) error { b.Reset(); return nil }, // rewind on reuse
)

buf := buffers.Get() // a ready, empty *bytes.Buffer
defer buffers.Put(buf)
```

## Install

```sh
go get github.com/JohanLindvall/objpool
```

Requires Go 1.18 or later.

## Why

`sync.Pool` is untyped, so every call site repeats two decisions: the type
assertion on `Get`, and what to do when the pool is empty — and call sites that
each make them on their own drift apart. Folding the element type into the pool
makes `Get` return a ready `*T`: a mis-asserted type becomes a compile error,
and the empty-pool path cannot be forgotten. The same goes for rewinding a
recycled element: the pool does it, so no call site can forget to.

## The element contract

Both pools are built from the same two functions:

- **`newFn func() *T`** builds an element whenever `Get` has none to reuse. It
  runs on the caller's goroutine and must return a ready, non-nil element.
- **`resetFn func(*T) error`** rewinds a *recycled* element before `Get`
  returns it. It never runs on a freshly built element, so required setup
  belongs in `newFn`. Pass `nil` when there is nothing to rewind, or when
  rewinding needs an argument only the call site has — a pooled `gzip.Writer`
  is reset onto its destination by the caller, after `Get`.

The reset runs in `Get`, not `Put`, on purpose: a reset that fails can then be
answered with a fresh element. Returning an error from `resetFn` declares the
element unfit to reuse; `Get` drops it and builds a replacement, and the caller
never sees the error. So `Get` never returns nil, and never a half-rewound
element.

That guarantee rests on `newFn`, so breaking it fails early and loudly: the
constructor panics on a nil `newFn` — at construction, typically program start,
rather than on the first miss under load — and `Get` panics if `newFn` returns
nil, naming the cause instead of handing back a nil that dereferences in
unrelated code. `Put(nil)` is ignored.

Both pools are safe for concurrent use; the elements they hand out are not
shared. Create them with their constructors — the zero values are not usable —
and do not copy them after first use.

## Pool

`Pool[T]` keeps `sync.Pool`'s semantics, including the one that matters most:
`Put` is a hint, not storage. The runtime may drop a pooled element at any
time, and drops every idle one within two garbage collections, so nothing whose
loss would matter may live only in a `Pool`.

That is the right trade for many small, cheap elements. It silently defeats
reuse, though, once collections outpace an element's use cycle: at a high
allocation rate the collector runs several times per cycle, so a large element
is rebuilt nearly every time — and the rebuilds' allocations then drive the very
collections that evict it. That is what `FreeList` is for.

## FreeList

`FreeList[T]` never drops an element it is given: a parked element survives any
number of collections, and `Get` hands back the most recently parked one.

```go
buffers := objpool.NewFreeList(64, // park at most 64 idle buffers
	func() *bytes.Buffer { return bytes.NewBuffer(make([]byte, 0, 1<<20)) },
	func(b *bytes.Buffer) error { b.Reset(); return nil }, // keeps the capacity
)
```

It needs no cap to stay bounded: `Get` builds only when the list is empty, so
the number of elements never exceeds the peak number checked out at once. The
price is that memory stays at that peak. `maxIdle` is defense in depth on top: a
`Put` that finds `maxIdle` elements already parked drops its element
(`maxIdle <= 0` means no limit). Set it well above peak concurrency, or it
reintroduces the rebuild churn the list exists to prevent.

Reuse is the whole point, so a reset must keep the capacity the list exists to
keep warm: truncate a buffer to length zero; don't replace it.

## RatchetTrim

A `FreeList` keeps whatever it is given, forever — including buffers that a
burst grew and the quieter traffic after it never uses again. `RatchetTrim`
decides, at release, when such an element should be dropped instead of put
back:

```go
type scratch struct {
	buf  []byte
	trim objpool.RatchetTrim // one policy per element
}

scratches := objpool.NewFreeList(0,
	func() *scratch { return &scratch{trim: objpool.DefaultRatchetTrim()} },
	func(s *scratch) error { s.buf = s.buf[:0]; return nil },
)

// release puts s back, unless its buffer has sat mostly unused for too long.
release := func(s *scratch) {
	if s.trim.Due(cap(s.buf), len(s.buf)) {
		return // drop: the garbage collector reclaims the oversized buffer
	}
	scratches.Put(s)
}
```

A cycle is *wasteful* when the element retains at least `Floor` bytes and more
than `Factor` times what the cycle used; `Strikes` consecutive wasteful cycles
make `Due` report drop, and one honest cycle resets the count. So a burst is
forgiven quickly, while a footprint nothing uses any more is released within a
few cycles. `DefaultRatchetTrim` drops an element retaining at least 16 MiB once
it has used less than a quarter of its footprint for 8 consecutive cycles; the
zero value never trims.

## Choosing

| | `Pool[T]` | `FreeList[T]` |
| --- | --- | --- |
| Built on | `sync.Pool` | a mutex-guarded LIFO stack |
| Idle elements | dropped at the runtime's discretion, all within two GCs | kept until drawn |
| Bounded by | garbage collection | peak concurrent checkouts, and optionally `maxIdle` |
| `Put` is | a hint | a guarantee, up to `maxIdle` |
| Suits | many small, cheap elements | few large, expensive-to-rebuild elements |

## API overview

| Symbol | Description |
| --- | --- |
| `New(newFn, resetFn) *Pool[T]` | Create a `Pool`. |
| `(*Pool[T]) Get() *T` | A recycled element, rewound, or a fresh one. Never nil. |
| `(*Pool[T]) Put(v *T)` | Offer `v` back for reuse. |
| `NewFreeList(maxIdle, newFn, resetFn) *FreeList[T]` | Create a `FreeList`. |
| `(*FreeList[T]) Get() *T` | The most recently parked element, rewound, or a fresh one. Never nil. |
| `(*FreeList[T]) Put(v *T)` | Park `v`, or drop it past `maxIdle`. |
| `RatchetTrim` | Drop policy with `Floor`, `Factor` and `Strikes`; one per element. |
| `DefaultRatchetTrim() RatchetTrim` | A 16 MiB floor, factor 4, 8 strikes. |
| `(*RatchetTrim) Due(retained, used int) bool` | Record a cycle; report whether to drop the element. |

The [package documentation](https://pkg.go.dev/github.com/JohanLindvall/objpool)
has the full contracts and runnable examples.

## Releases

CI tags every green commit on `main` with the next patch version; minor and
major versions are tagged by hand.

## License

[MIT](LICENSE)
