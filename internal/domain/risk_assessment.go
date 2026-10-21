package domain

// Severity represents the Policy Engine severity level.
type Severity string

const (
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

// RiskAssessment stores the frozen policy evaluation for a review.
type RiskAssessment struct {
	ID              RiskAssessmentID
	ReviewID        ReviewID
	HighestSeverity Severity
	CountBySeverity map[Severity]int
	PolicyVersion   string
	Rationale       string
}
