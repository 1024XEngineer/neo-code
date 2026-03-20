package memory

import (
	"math"
	"sort"
)

type Match struct {
	Item  Item
	Score float64
}

func Search(items []Item, query []float64, topK int, minScore float64) []Match {
	if topK <= 0 || len(query) == 0 {
		return nil
	}

	matches := make([]Match, 0, len(items))
	for _, item := range items {
		score := cosineSimilarity(query, item.Embedding)
		if score < minScore {
			continue
		}
		matches = append(matches, Match{Item: item, Score: score})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	if len(matches) > topK {
		matches = matches[:topK]
	}

	return matches
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return -1
	}

	var dot float64
	var normA float64
	var normB float64

	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return -1
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
