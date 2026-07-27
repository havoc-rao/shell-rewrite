// shr — shell command shortener.
// 在执行前按规则改写命令：git co → git checkout，支持多层（colink data u → colink data upload）。
package main

import (
	"os"

	"github.com/havoc-rao/shell-rewrite/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
