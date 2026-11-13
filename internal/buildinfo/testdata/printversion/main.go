// Command printversion prints buildinfo.Version(), for TestLinkerFlag.
package main

import (
	"fmt"

	"github.com/cerebrai-app/go-cerebrai/internal/buildinfo"
)

func main() {
	fmt.Println(buildinfo.Version())
}
