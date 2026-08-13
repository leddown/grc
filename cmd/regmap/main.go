// Command regmap maps a cybersecurity regulation or standard to NIST SP 800-53
// Rev 5 controls, with a human review gate at every step where machine output
// could be wrong.
//
// It is a standalone operator tool, separate from the grc web application, but
// shares this module's embedded NIST control catalog so both surfaces map
// against the same controls.
package main

import (
	"fmt"
	"os"

	"grc/internal/regmap/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
