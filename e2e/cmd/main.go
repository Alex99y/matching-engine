// Command e2e is a build placeholder so this module has a cmd/ like every other module and
// `make build` has a target. The suite itself is driven by `go test` against a running
// stack — see e2e/README.md and e2e/PLAN.md.
package main

import "github.com/alex99y/matching-engine/common/pkg/logger"

func main() {
	logger.NewLogger(logger.Info).Info(
		"e2e: run the suite with `make -C e2e test-e2e` against a live api+core+db+rabbitmq stack; this binary does nothing",
	)
}
