package objpool

// RatchetTrim is a drop policy for pooled elements whose retained memory can
// ratchet upward: a burst grows an element's buffers, the quieter traffic after
// it never uses that capacity again, and a FreeList keeps it forever. The
// decision belongs to the caller at release: keep one RatchetTrim in each
// pooled element, call Due as the element is released, and drop the element
// instead of putting it back when Due reports true.
//
// A cycle is wasteful when the element retains at least Floor bytes and more
// than Factor times what the cycle used; Strikes consecutive wasteful cycles
// make Due report drop. The floor exempts elements small enough that a rebuild
// would cost more than the memory it reclaims — size alone, regardless of how
// busy the element is. The strikes forgive a burst quickly, while a footprint
// the traffic after it never uses is released within a few cycles.
//
// The zero value never trims; DefaultRatchetTrim returns a tuned policy. A
// RatchetTrim is not safe for concurrent use: like the element it lives on, it
// has one holder at a time.
type RatchetTrim struct {
	// Floor exempts elements retaining fewer bytes: no cycle below it is
	// wasteful.
	Floor int
	// Factor is the retained/used ratio above which a cycle is wasteful; a
	// Factor <= 0 makes every cycle at or above Floor wasteful.
	Factor int
	// Strikes is how many consecutive wasteful cycles make Due report drop;
	// Strikes <= 0 disables the policy, which is why the zero value never trims.
	Strikes int

	streak int // consecutive wasteful cycles so far
}

// DefaultRatchetTrim returns a policy that drops an element retaining at least
// 16 MiB once it has used less than a quarter of its footprint for 8
// consecutive cycles.
func DefaultRatchetTrim() RatchetTrim {
	return RatchetTrim{Floor: 16 << 20, Factor: 4, Strikes: 8}
}

// Due records one cycle and reports whether the element should be dropped
// rather than put back. retained is the element's kept footprint in bytes; used
// is what the cycle's work actually needed. One honest cycle — capacity
// genuinely used — resets the count, so sustained heavy traffic never cycles
// elements.
//
// A cycle with used == 0 is the most wasteful case, not an exemption: an
// element above the floor that did no work for Strikes consecutive cycles holds
// its memory for no benefit at all. That cannot punish an element for sitting
// idle in a FreeList: Due runs only when a holder releases the element, never
// while it is parked.
func (t *RatchetTrim) Due(retained, used int) bool {
	if t.Strikes <= 0 {
		return false
	}
	if !t.wasteful(retained, used) {
		t.streak = 0
		return false
	}
	t.streak++
	return t.streak >= t.Strikes
}

// wasteful reports whether retained is at least Floor and more than Factor
// times used.
func (t *RatchetTrim) wasteful(retained, used int) bool {
	if retained < t.Floor {
		return false
	}
	if t.Factor <= 0 {
		return true
	}
	// Divide before multiplying: once used exceeds retained/Factor the product
	// is certainly above retained, and computing it could overflow int (32 bits
	// on some platforms) into a false strike.
	return used <= retained/t.Factor && retained > used*t.Factor
}
