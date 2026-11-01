package barego

func Start() {
	go func() {}() // want `bare go statement`
}
