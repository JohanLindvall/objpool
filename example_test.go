package objpool_test

import (
	"bytes"
	"fmt"
	"runtime"

	"github.com/JohanLindvall/objpool"
)

func ExampleNew() {
	buffers := objpool.New(
		func() *bytes.Buffer { return new(bytes.Buffer) },
		func(b *bytes.Buffer) error { b.Reset(); return nil },
	)

	buf := buffers.Get() // a ready *bytes.Buffer: never nil, no type assertion
	buf.WriteString("hello")
	fmt.Println(buf)
	buffers.Put(buf)

	// Recycled and rewound, or freshly built: either way it comes back empty.
	fmt.Println(buffers.Get().Len())

	// Output:
	// hello
	// 0
}

func ExampleNewFreeList() {
	builds := 0
	buffers := objpool.NewFreeList(8,
		func() *bytes.Buffer {
			builds++
			return bytes.NewBuffer(make([]byte, 0, 1<<20))
		},
		// Truncate, keeping the grown capacity the list exists to preserve.
		func(b *bytes.Buffer) error { b.Reset(); return nil },
	)

	for i := 0; i < 3; i++ {
		buf := buffers.Get()
		buf.WriteString("payload")
		buffers.Put(buf)
		runtime.GC() // would empty a Pool; the FreeList keeps its element
	}
	fmt.Println("builds:", builds)

	// Output: builds: 1
}

func ExampleRatchetTrim() {
	// job is a pooled element whose buffer grows to fit the largest input it has seen.
	type job struct {
		buf  []byte
		trim objpool.RatchetTrim
	}

	jobs := objpool.NewFreeList(0,
		func() *job {
			// A small policy to keep the example light; DefaultRatchetTrim suits
			// elements that grow to megabytes.
			return &job{trim: objpool.RatchetTrim{Floor: 1 << 10, Factor: 4, Strikes: 3}}
		},
		func(j *job) error { j.buf = j.buf[:0]; return nil },
	)

	// release puts j back, or drops it once its buffer has sat mostly unused for
	// Strikes consecutive cycles. It reports whether j was kept.
	release := func(j *job) bool {
		if j.trim.Due(cap(j.buf), len(j.buf)) {
			return false // dropped: the garbage collector reclaims the buffer
		}
		jobs.Put(j)
		return true
	}

	j := jobs.Get()
	j.buf = append(j.buf, make([]byte, 64<<10)...) // a burst grows the buffer to 64 KiB
	fmt.Println("burst: kept =", release(j))

	for i := 1; i <= 3; i++ {
		j = jobs.Get()
		j.buf = append(j.buf, "small"...) // quiet traffic uses a sliver of it
		fmt.Printf("quiet %d: kept = %v\n", i, release(j))
	}

	// Output:
	// burst: kept = true
	// quiet 1: kept = true
	// quiet 2: kept = true
	// quiet 3: kept = false
}
