package settings

import "os"

func URL() string { u, _ := os.LookupEnv("ATLAS_URL"); return u }
