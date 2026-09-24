package objpool

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type box struct {
	n    int
	held int32 // 1 while a holder has the box; see hammer
}

// hammer cycles Get and Put from several goroutines, for the race detector, and
// fails if an element is ever handed to two holders at once.
func hammer(t *testing.T, get func() *box, put func(*box)) {
	t.Helper()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				v := get()
				if !atomic.CompareAndSwapInt32(&v.held, 0, 1) {
					t.Error("element handed to two holders at once")
					return
				}
				runtime.Gosched() // widen the window a second holder would land in
				atomic.StoreInt32(&v.held, 0)
				put(v)
			}
		}()
	}
	wg.Wait()
}

// Test_unit_Pool_GetBuildsWhenEmpty: an empty pool builds with newFn, so Get never
// hands back nil and callers need no fallback of their own.
func Test_unit_Pool_GetBuildsWhenEmpty(t *testing.T) {
	built := 0
	p := New(func() *box {
		built++
		return &box{n: 42}
	}, nil)

	v := p.Get()
	require.NotNil(t, v)
	assert.Equal(t, 42, v.n)
	assert.Equal(t, 1, built)
}

// Test_unit_Pool_PutMakesValueAvailable: a Put element is reused rather than
// rebuilt. sync.Pool gives no Put->Get identity guarantee — a GC can drop any
// element, a Get on another P can miss it, and under -race the runtime
// deliberately drops a random quarter of Puts to catch code relying on
// retention. So this asserts reuse statistically: across many cycles the pool
// must build far fewer elements than it hands out, never that one particular
// Put comes back.
func Test_unit_Pool_PutMakesValueAvailable(t *testing.T) {
	built := 0
	p := New(func() *box {
		built++
		return &box{}
	}, nil)

	const cycles = 10000
	for i := 0; i < cycles; i++ {
		p.Put(p.Get())
	}
	// Plain runs build ~1; -race rebuilds ~25%. Equal to cycles would mean no
	// reuse ever happened — the one outcome that must fail.
	assert.Less(t, built, cycles, "the pool never reused a single element")
}

// Test_unit_Pool_PutNilIgnored: Put tolerates nil so callers need no guard — and
// a nil must never reach the pool, or a later Get would return one despite the
// *T contract.
func Test_unit_Pool_PutNilIgnored(t *testing.T) {
	p := New(func() *box { return &box{n: 1} }, nil)
	p.Put(nil)
	for i := 0; i < 10; i++ {
		require.NotNil(t, p.Get(), "a nil Put must not become a nil Get")
	}
}

// Test_unit_Pool_NilNewFnPanicsAtConstruction: Get's never-nil guarantee rests on
// newFn, so a nil one must fail at New — typically program start — rather than on
// the first pool miss under load.
func Test_unit_Pool_NilNewFnPanicsAtConstruction(t *testing.T) {
	assert.PanicsWithValue(t, "objpool.New: newFn must not be nil", func() {
		New[box](nil, nil)
	})
}

// Test_unit_Pool_NilFromNewFnPanics: a newFn returning nil breaks the *T contract,
// so it panics inside the miss path naming the cause, rather than handing back a
// nil that dereferences somewhere unrelated.
func Test_unit_Pool_NilFromNewFnPanics(t *testing.T) {
	p := New(func() *box { return nil }, nil)
	assert.PanicsWithValue(t, "objpool.New: newFn returned nil", func() {
		p.Get()
	})
}

func Test_unit_Pool_Concurrent(t *testing.T) {
	p := New(func() *box { return &box{} }, nil)
	hammer(t, p.Get, p.Put)
}

// Test_unit_Pool_ResetRewindsRecycledOnly pins the Get-side reset contract: a
// recycled element is rewound before the caller sees it, and a freshly built one
// is not — newFn already returns a ready element, so running the reset over it
// would make the reset the place required setup lives, and hide a newFn that
// forgot it.
func Test_unit_Pool_ResetRewindsRecycledOnly(t *testing.T) {
	resets := 0
	p := New(
		func() *box { return &box{n: 42} },
		func(b *box) error { resets++; b.n = 0; return nil },
	)

	v := p.Get()
	assert.Equal(t, 42, v.n, "a freshly built element must reach the caller untouched")
	assert.Equal(t, 0, resets, "reset must not run on a fresh build")

	// sync.Pool may drop any Put (under -race, a random quarter of them), so cycle
	// until a Get draws the parked element back.
	for i := 0; i < 100; i++ {
		v.n = 99
		p.Put(v)
		got := p.Get()
		if got == v {
			assert.Equal(t, 0, got.n, "a recycled element must be rewound before Get returns it")
			return
		}
		v = got // a miss: Get built afresh
	}
	t.Fatal("sync.Pool never handed a parked element back")
}

// Test_unit_Pool_ResetErrorDropsAndRebuilds: a reset that reports the element
// unfit must not hand it back half-rewound. The element is dropped and a
// replacement built, so Get's never-nil contract holds and the caller never
// learns of the error.
func Test_unit_Pool_ResetErrorDropsAndRebuilds(t *testing.T) {
	resets := 0
	p := New(
		func() *box { return &box{n: 42} },
		func(*box) error { resets++; return errors.New("unfit") },
	)

	// As above, cycle until a Get draws a parked element and its reset fails. A
	// miss and a failed reset both end in a fresh build, so every Get must.
	v := p.Get()
	for i := 0; i < 100 && resets == 0; i++ {
		v.n = 7
		p.Put(v)
		got := p.Get()
		assert.NotSame(t, v, got, "an element whose reset failed must not be handed back")
		assert.Equal(t, 42, got.n, "the replacement must be a fresh build")
		v = got
	}
	require.Equal(t, 1, resets, "sync.Pool never handed a parked element back")
}
