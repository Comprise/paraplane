package pathset

import (
	"strings"
	"testing"
)

var (
	benchAdv    = strings.Repeat("[0-ÿ]", 48) + ".go" // 243 runes, 48 classes of 208 runes
	benchAdvLit = strings.Repeat("q", 48) + ".go"
)

func BenchmarkOverlapAdversarial(b *testing.B) {
	for range b.N {
		Overlap(benchAdv, benchAdvLit)
	}
}

func BenchmarkTouchesAdversarial(b *testing.B) {
	owns := []string{benchAdvLit}
	for range b.N {
		Touches(owns, benchAdv)
	}
}

func BenchmarkOverlapTypical(b *testing.B) {
	for range b.N {
		Overlap("internal/[a-z]*/*.go", "internal/plan/plan.go")
	}
}

func BenchmarkTouchesTypical(b *testing.B) {
	owns := []string{"web/package.json"}
	for range b.N {
		Touches(owns, "**/package.json")
	}
}
