// Package version identifies the build running in production.
package version

// Version is injected from the GitHub release tag at build time, without its v
// prefix. It is embedded in the SSH banner, which is where the arcade router's
// health prober reads it (../ssh-arcadelobby/docs/03-games-registry-and-health.md).
// Local builds deliberately identify themselves as development builds.
var Version = "dev"
