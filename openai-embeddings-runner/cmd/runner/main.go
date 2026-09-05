package main

import "github.com/Cloud-SPE/livepeer-modules-openai-runners/openai-embeddings-runner/internal/runner"

// version is stamped at build time (-X main.version=...) from the git tag
// or sha; "dev" means a bare `go build`.
var version = "dev"

func main() {
	runner.Version = version
	runner.Run()
}
