package main

import (
	"os"

	"github.com/feloex/GoogleTakeoutFixer/internal/cli"
	"github.com/feloex/GoogleTakeoutFixer/internal/gui"
)

func main() {
	// If args are provided, run cli
	if len(os.Args) > 1 {
		cli.Main()
		return
	}

	gui.Main()
}
