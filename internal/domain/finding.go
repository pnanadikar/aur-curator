package domain

import "encoding/json"

// Finding represents one analyzer observation captured during a review.
type Finding struct {
	ID       FindingID
	ReviewID ReviewID
	Analyzer string
	Category string
	Summary  string
	Details  json.RawMessage
	Location string
}
