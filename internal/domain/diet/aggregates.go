package diet

import "time"

// DietPlan is the aggregate root — the complete output of the diet command.
type DietPlan struct {
	Entries    []DietEntry
	Summary    DietSummary
	SBOMPath   string
	SourceRoot string
	AnalyzedAt time.Time
}

// DietSummary provides aggregate statistics for the diet plan.
type DietSummary struct {
	TotalDirect              int
	TotalTransitive          int
	TotalExclusiveTransitive int
	UnusedDirect             int
	EasyWins                 int
	EstimatedRemovable       int
	StaysAsIndirectCount     int
}
