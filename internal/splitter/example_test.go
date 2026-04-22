package splitter_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/veggiemonk/sflit/internal/splitter"
)

func ExampleConfig_Validate() {
	cfg := splitter.Config{Source: "big.go", Sink: "small.go"}
	fmt.Println(cfg.Validate())

	cfg.Regex = "^Filter"
	fmt.Println(cfg.Validate())

	cfg.Regex = "[invalid"
	fmt.Println(cfg.Validate())
	// Output:
	// at least one of -regex or -receiver is required
	// <nil>
	// invalid -regex: error parsing regexp: missing closing ]: `[invalid`
}

// ExampleRun copies a single function from source to sink. The source is
// unchanged because Move is false.
func ExampleRun() {
	dir, err := os.MkdirTemp("", "sflit-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	src := filepath.Join(dir, "big.go")
	sink := filepath.Join(dir, "filter.go")
	_ = os.WriteFile(src, []byte(`package p

func FilterA() {}
func Other()   {}
`), 0o600)

	res, err := splitter.Run(splitter.Config{
		Source: src,
		Sink:   sink,
		Regex:  "^Filter",
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("matched:", res.Matched)
	fmt.Println("move:", res.Move)
	// Output:
	// matched: [FilterA]
	// move: false
}

// ExampleRun_moveReceiver moves a type and every method defined on it.
func ExampleRun_moveReceiver() {
	dir, err := os.MkdirTemp("", "sflit-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	src := filepath.Join(dir, "big.go")
	sink := filepath.Join(dir, "my_struct.go")
	_ = os.WriteFile(src, []byte(`package p

type MyStruct struct{ N int }

func (m MyStruct) Filter() bool { return m.N > 0 }
func (m MyStruct) Double() int  { return m.N * 2 }

func Keep() {}
`), 0o600)

	res, err := splitter.Run(splitter.Config{
		Source:   src,
		Sink:     sink,
		Receiver: "MyStruct",
		Move:     true,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(res.Matched)
	// Output:
	// [type MyStruct MyStruct.Filter MyStruct.Double]
}
