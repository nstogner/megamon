package version

import "fmt"

// These variables are set via -ldflags during the build.
// Example: -ldflags "-X example.com/megamon/pkg/version.Version=1.2.3 -X example.com/megamon/pkg/version.Commit=abcdef"
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a formatted version string.
func String() string {
	return fmt.Sprintf("%s-%s (%s)", Version, Commit, Date)
}
