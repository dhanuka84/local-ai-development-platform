package domain

import (
	"math"
	"testing"
)

func TestEmbeddingQuality(t *testing.T) {
	for _, vector := range [][]float32{nil, {}, {0, 0}, {float32(math.NaN()), 1}, {1, float32(math.Inf(1))}} {
		if err := ValidateEmbedding(vector, 2); err == nil {
			t.Fatalf("invalid vector accepted: %v", vector)
		}
	}
	if err := ValidateEmbedding([]float32{1, -1}, 2); err != nil {
		t.Fatal(err)
	}
}
