// Test-only fixture for a retained owner with the pre-#76 catalog identity.
package main

import (
	"os"

	"github.com/Bochner/burrow/core/connection"
	"github.com/vibepwners/hovel/sdk/go/hovel"
)

type legacyModule struct{ connection.Module }

func (legacyModule) Info() hovel.Info {
	info := (connection.Module{}).Info()
	info.Name = "burrow-connection"
	return info
}

func main() {
	if os.Getenv("BURROW_ASKPASS") == "1" {
		if len(os.Args) != 2 || connection.Askpass(os.Args[1]) != nil {
			os.Exit(1)
		}
		return
	}
	hovel.Serve(legacyModule{})
}
