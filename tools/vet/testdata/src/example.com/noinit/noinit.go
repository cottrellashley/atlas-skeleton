package noinit

func init() {} // want `init function: register in the deployable's one list instead`

// atlas:allow-init the one documented exception
func init() {}
