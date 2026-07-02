// Genesis Agent Desktop 正式产品入口。
package main

import (
	"context"
	"fmt"
	"os"

	"genesis-agent/products/desktop/bootstrap"
)

func main() {
	if err := bootstrap.Execute(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
