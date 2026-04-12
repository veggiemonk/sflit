// Package version handles the version from VCS automatically.
// source: https://github.com/imjasonh/version/blob/main/version.go
package version

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
)

type Version struct {
	Revision string `json:"revision"`
	Version  string `json:"version"`
	Time     string `json:"time"`
	Dirty    bool   `json:"dirty"`
}

var ver = Version{
	Revision: "unknown",
	Version:  "unknown",
	Time:     "unknown",
	Dirty:    false,
}

func (v Version) String() string {
	return fmt.Sprintf(`Revision: %s
Version: %s
BuildTime: %s
Dirty: %t`, v.Revision, v.Version, v.Time, v.Dirty)
}

func (v Version) AsJSON() string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(b)
}

var initVersion = sync.OnceFunc(func() {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		slog.Warn("version: no build info detected")
		return
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		ver.Version = bi.Main.Version
	}
	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			ver.Revision = setting.Value
		case "vcs.time":
			ver.Time = setting.Value
		case "vcs.modified":
			ver.Dirty = setting.Value == "true"
		}
	}
})

func Get() Version {
	initVersion()
	return ver
}
