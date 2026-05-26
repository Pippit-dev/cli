package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Version is set by GoReleaser for release binaries.
var Version = "dev"

const packageName = "@pippit-dev/cli"

type packageJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func Current() string {
	if strings.TrimSpace(Version) != "" && Version != "dev" {
		return strings.TrimSpace(Version)
	}
	if version := packageVersionFromWorktree(); version != "" {
		return version
	}
	return Version
}

func packageVersionFromWorktree() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		version := packageVersion(filepath.Join(dir, "package.json"))
		if version != "" {
			return version
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func packageVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	if pkg.Name != packageName {
		return ""
	}
	return strings.TrimSpace(pkg.Version)
}
