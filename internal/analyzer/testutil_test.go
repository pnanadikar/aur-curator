package analyzer

import "github.com/pramod/aur-curator/internal/domain"

func countCategory(findings []domain.Finding, category string) int {
	n := 0
	for _, f := range findings {
		if f.Category == category {
			n++
		}
	}
	return n
}
