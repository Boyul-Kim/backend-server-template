// Package version holds build metadata injected at link time:
//
//	go build -ldflags "-X github.com/boyulkim/api-template/internal/version.Version=v1.2.3"
package version

var (
	Version = "dev"
	Commit  = "none"
)
