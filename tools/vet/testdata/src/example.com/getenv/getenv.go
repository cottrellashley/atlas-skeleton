package getenv

import "os"

func URL() string {
	return os.Getenv("ATLAS_URL") // want `os.Getenv outside a composition root or settings`
}
