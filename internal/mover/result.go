package mover

// Result holds the outcome of a successful Run.
type Result struct {
	Source                string   `json:"source"`
	Sink                  string   `json:"sink"`
	Matched               []string `json:"matched"`
	DeclarationsRemaining int      `json:"declarations_remaining"`
	Move                  bool     `json:"move"`
	// Attempts is how many pipeline attempts the run took. 1 means no
	// commit-time conflict was encountered; higher values mean concurrent
	// writers landed between parse and commit and the pipeline re-ran
	// (ADR-0001) — observability for orchestrators fanning out runs.
	Attempts int `json:"attempts"`
}
