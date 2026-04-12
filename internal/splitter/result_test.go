package splitter

import (
	"encoding/json"
	"testing"
)

func TestResult_JSON(t *testing.T) {
	r := Result{
		Source:                "big.go",
		Sink:                  "small.go",
		Move:                  true,
		Matched:               []string{"FilterA", "FilterB"},
		DeclarationsRemaining: 5,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got Result
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Source != "big.go" || got.Sink != "small.go" || !got.Move ||
		len(got.Matched) != 2 || got.Matched[0] != "FilterA" ||
		got.DeclarationsRemaining != 5 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}
