package rank

import (
	"math"
	"testing"
)

func TestRankWindowRejectsInt32Overflow(t *testing.T) {
	if _, _, err := rankWindow(math.MaxInt32, 2); err == nil {
		t.Fatal("rankWindow accepted an overflowing start plus count")
	}
}
