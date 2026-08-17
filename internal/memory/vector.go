package memory

import "math"

// Normalise scales a vector to unit length, in place, and returns it.
//
// Every vector is normalised before it is stored, which turns cosine similarity
// into a plain dot product at query time. That removes two square roots and a
// division from the inner loop of every comparison — the loop that runs once
// per memory in the library.
//
// A zero vector cannot be normalised; it is left alone and will simply never
// match anything, which is the correct behaviour for an empty embedding.
func Normalise(v []float32) []float32 {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	if sum == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
	return v
}

// dot is the similarity of two normalised vectors.
func dot(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// scored is one candidate during a search.
type scored struct {
	id    string
	score float32
}

// topK keeps the k best scores seen, by insertion into a small sorted slice.
//
// A heap would be the reflex here, but k is 5 to 20 and the comparison against
// the current worst rejects almost every candidate in one branch. Insertion
// only happens for the rare improvement.
type topK struct {
	k    int
	best []scored
}

func newTopK(k int) *topK {
	if k < 1 {
		k = 1
	}
	return &topK{k: k, best: make([]scored, 0, k)}
}

func (t *topK) add(id string, score float32) {
	if len(t.best) == t.k && score <= t.best[len(t.best)-1].score {
		return
	}
	if len(t.best) < t.k {
		t.best = append(t.best, scored{})
	}
	i := len(t.best) - 1
	for i > 0 && t.best[i-1].score < score {
		t.best[i] = t.best[i-1]
		i--
	}
	t.best[i] = scored{id: id, score: score}
}

func (t *topK) results() []scored { return t.best }
