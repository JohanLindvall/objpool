package objpool

import (
	"errors"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_unit_FreeList_SurvivesGC pins the property that distinguishes FreeList from
// Pool, and the reason it exists: a parked element is returned by Get even after
// several GC cycles. Under sync.Pool the same sequence hands back a fresh build,
// since two collections empty it.
func Test_unit_FreeList_SurvivesGC(t *testing.T) {
	built := 0
	l := NewFreeList(0, func() *box {
		built++
		return &box{}
	}, nil)

	v := l.Get()
	v.n = 7
	l.Put(v)

	for i := 0; i < 4; i++ {
		runtime.GC()
	}

	got := l.Get()
	assert.Same(t, v, got, "a parked element must survive GC — that is the whole point")
	assert.Equal(t, 7, got.n)
	assert.Equal(t, 1, built)
}

// Test_unit_FreeList_SelfBounding: the list never grows past the peak number of
// elements checked out at once, because Get only builds on an empty list.
func Test_unit_FreeList_SelfBounding(t *testing.T) {
	built := 0
	l := NewFreeList(0, func() *box {
		built++
		return &box{}
	}, nil)

	// A peak of 3 checked out → exactly 3 builds, however many cycles follow.
	a, b, c := l.Get(), l.Get(), l.Get()
	l.Put(a)
	l.Put(b)
	l.Put(c)
	for i := 0; i < 100; i++ {
		l.Put(l.Get())
	}
	assert.Equal(t, 3, built)
	assert.Len(t, l.free, 3)
}

// Test_unit_FreeList_LIFO: Get hands back the most recently parked element, so
// steady traffic keeps cycling the same warm elements.
func Test_unit_FreeList_LIFO(t *testing.T) {
	l := NewFreeList(0, func() *box { return &box{} }, nil)
	a, b := l.Get(), l.Get()
	l.Put(a)
	l.Put(b)
	assert.Same(t, b, l.Get())
	assert.Same(t, a, l.Get())
}

// Test_unit_FreeList_MaxIdleCap: a Put past the cap drops its element instead of
// parking it, so a miscounted caller cannot grow the list without bound; Get is
// never limited, and maxIdle <= 0 means no cap at all.
func Test_unit_FreeList_MaxIdleCap(t *testing.T) {
	built := 0
	l := NewFreeList(2, func() *box {
		built++
		return &box{}
	}, nil)

	a, b, c := l.Get(), l.Get(), l.Get()
	require.Equal(t, 3, built, "Get is not capped")
	l.Put(a)
	l.Put(b)
	l.Put(c) // over the cap: dropped
	assert.Len(t, l.free, 2)

	// Draining the list and going one deeper rebuilds exactly the dropped one.
	_, _, _ = l.Get(), l.Get(), l.Get()
	assert.Equal(t, 4, built)

	unbounded := NewFreeList(-1, func() *box { return &box{} }, nil)
	for i := 0; i < 3; i++ {
		unbounded.Put(&box{})
	}
	assert.Len(t, unbounded.free, 3, "a negative maxIdle must not cap the list")
}

// Test_unit_FreeList_NilContracts: a nil newFn fails at construction, a newFn
// returning nil fails on the miss that calls it, and a nil Put is ignored rather
// than parked.
func Test_unit_FreeList_NilContracts(t *testing.T) {
	assert.PanicsWithValue(t, "objpool.NewFreeList: newFn must not be nil", func() {
		NewFreeList[box](0, nil, nil)
	})

	l := NewFreeList(0, func() *box { return nil }, nil)
	assert.PanicsWithValue(t, "objpool.NewFreeList: newFn returned nil", func() {
		l.Get()
	})

	ok := NewFreeList(0, func() *box { return &box{n: 1} }, nil)
	ok.Put(nil)
	assert.Empty(t, ok.free, "a nil Put must not be parked")
	require.NotNil(t, ok.Get())
}

func Test_unit_FreeList_Concurrent(t *testing.T) {
	l := NewFreeList(0, func() *box { return &box{} }, nil)
	hammer(t, l.Get, l.Put)
}

// Test_unit_FreeList_ResetRewindsParkedOnly mirrors the Pool contract, and matters
// more here: a FreeList element is guaranteed to come back, so an un-rewound one
// would carry the previous holder's state into every next cycle rather than
// merely sometimes.
func Test_unit_FreeList_ResetRewindsParkedOnly(t *testing.T) {
	resets := 0
	l := NewFreeList(
		0,
		func() *box { return &box{n: 42} },
		func(b *box) error { resets++; b.n = 0; return nil },
	)

	fresh := l.Get()
	assert.Equal(t, 42, fresh.n, "a freshly built element must reach the caller untouched")
	assert.Equal(t, 0, resets, "reset must not run on a fresh build")

	fresh.n = 99
	l.Put(fresh)

	got := l.Get()
	assert.Same(t, fresh, got, "the list must hand back the parked element")
	assert.Equal(t, 0, got.n, "a parked element must be rewound before Get returns it")
	assert.Equal(t, 1, resets)
}

// Test_unit_FreeList_ResetErrorDropsAndRebuilds: an element whose reset fails is
// dropped from the list rather than handed back, and does not linger to be handed
// out later.
func Test_unit_FreeList_ResetErrorDropsAndRebuilds(t *testing.T) {
	built := 0
	l := NewFreeList(
		0,
		func() *box { built++; return &box{n: 42} },
		func(*box) error { return errors.New("unfit") },
	)

	first := l.Get()
	require.Equal(t, 1, built)
	l.Put(first)

	got := l.Get()
	assert.NotSame(t, first, got, "an element whose reset failed must not be handed back")
	assert.Equal(t, 2, built)
	assert.Empty(t, l.free, "the unfit element must not stay in the list")
}
