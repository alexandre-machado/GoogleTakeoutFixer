package main

import (
	"fmt"
	"os"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Flags missing! Enter InputPath (path of your takeout) and OutputPath (where your fixed files will be located).")
		return
	}

	var InputPath = os.Args[1]
	var OutputPath = os.Args[2]

	fixer.ProcessTakeout(InputPath, OutputPath)
}
