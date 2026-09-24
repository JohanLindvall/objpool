package objpool

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_unit_RatchetTrim(t *testing.T) {
	tr := DefaultRatchetTrim()
	big := tr.Floor * 8

	// Honest cycles — small, or genuinely utilized — never strike.
	assert.False(t, tr.Due(tr.Floor-1, 0), "below the floor is always kept")
	assert.False(t, tr.Due(big, big/tr.Factor), "retained within Factor x used is honest")

	// Wasteful cycles accumulate to a drop on the Strikes-th...
	for i := 1; i < tr.Strikes; i++ {
		assert.False(t, tr.Due(big, 0), "strike %d must not yet drop", i)
	}
	assert.True(t, tr.Due(big, 0))

	// ...and one honest cycle resets the count.
	tr = DefaultRatchetTrim()
	for i := 1; i < tr.Strikes; i++ {
		tr.Due(big, 0)
	}
	assert.False(t, tr.Due(big, big/2), "honest cycle resets")
	for i := 1; i < tr.Strikes; i++ {
		assert.False(t, tr.Due(big, 0), "post-reset strike %d must not drop", i)
	}
	assert.True(t, tr.Due(big, 0))
}

// Test_unit_RatchetTrim_Wasteful pins the per-cycle test exactly — at least Floor
// retained, and more than Factor times used — including where it divides instead
// of multiplying: used*Factor overflows int past MaxInt/Factor (512 MiB where int
// is 32 bits) and must not wrap into a false strike.
func Test_unit_RatchetTrim_Wasteful(t *testing.T) {
	for _, c := range []struct {
		name                          string
		floor, factor, retained, used int
		want                          bool
	}{
		{"below the floor", 100, 4, 99, 0, false},
		{"at the floor", 100, 4, 100, 0, true},
		{"exactly Factor x used", 0, 4, 400, 100, false},
		{"just above Factor x used", 0, 4, 401, 100, true},
		{"retained/Factor rounds down to used", 0, 4, 403, 100, true},
		{"used just above retained/Factor", 0, 4, 403, 101, false},
		{"fully used, product overflows int", 0, 4, math.MaxInt / 2, math.MaxInt / 2, false},
		{"Factor <= 0: every cycle at or above the floor", 100, 0, 100, math.MaxInt, true},
	} {
		tr := RatchetTrim{Floor: c.floor, Factor: c.factor, Strikes: 1}
		assert.Equal(t, c.want, tr.Due(c.retained, c.used), c.name)
	}
}

// Test_unit_RatchetTrim_ZeroValueDisabled: the zero value must be inert — a caller
// that forgets DefaultRatchetTrim gets no trimming, never immediate drops.
func Test_unit_RatchetTrim_ZeroValueDisabled(t *testing.T) {
	var tr RatchetTrim
	for i := 0; i < 100; i++ {
		assert.False(t, tr.Due(1<<30, 0))
	}
}
