package scorecard

// apiResponse represents the top-level JSON response from scorecard.dev.
type apiResponse struct {
	Date      string       `json:"date"`
	Repo      repoInfo     `json:"repo"`
	Scorecard versionInfo  `json:"scorecard"`
	Score     float64      `json:"score"`
	Checks    []checkEntry `json:"checks"`
}

type repoInfo struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type checkEntry struct {
	Name          string        `json:"name"`
	Score         int           `json:"score"`
	Reason        string        `json:"reason"`
	Details       []string      `json:"details"`
	Documentation documentation `json:"documentation"`
}

type documentation struct {
	Short string `json:"short"`
	URL   string `json:"url"`
}
