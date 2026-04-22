package splitter

// Result holds the outcome of a successful Run.
type Result struct {
	Source                string   `json:"source"`
	Sink                  string   `json:"sink"`
	Matched               []string `json:"matched"`
	DeclarationsRemaining int      `json:"declarations_remaining"`
	Move                  bool     `json:"move"`
}
