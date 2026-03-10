package analysis

// ScoreEntity represents an individual score within an analysis.
type ScoreEntity struct {
	name     string
	value    int
	maxValue int
	reason   string
}

// NewScoreEntity creates a new score entity.
func NewScoreEntity(name string, value, maxValue int, reason string) *ScoreEntity {
	return &ScoreEntity{name: name, value: value, maxValue: maxValue, reason: reason}
}

// Name returns the score name.
func (s *ScoreEntity) Name() string { return s.name }

// Value returns the score value.
func (s *ScoreEntity) Value() int { return s.value }

// MaxValue returns the maximum possible value for this score.
func (s *ScoreEntity) MaxValue() int { return s.maxValue }

// Reason returns the scoring reason.
func (s *ScoreEntity) Reason() string { return s.reason }
