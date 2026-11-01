package atlas

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"atlas.local/sdk/go/protocol"
)

// The environment a client reads (§9.7.2). Settings are read here, once,
// at Connect (atlasvet: never on the request path): the variables, and
// <ATLAS_HOME>/config.toml.
type environment struct {
	URL, Home, Env, Token string
	Conformance           bool
	// ConfigEnv and Environments are config.toml's env and its
	// [environments.<name>] urls.
	ConfigEnv    string
	Environments map[string]string
}

// readEnvironment reads the variables, from vars when it is given and the
// process's otherwise, and config.toml under home (else ATLAS_HOME, else
// ~/.atlas). A config.toml that cannot be read or parsed is a problem only
// when the gateway is chosen from it.
func readEnvironment(home string, vars map[string]string) (environment, error) {
	getenv := os.Getenv
	if vars != nil {
		getenv = func(k string) string { return vars[k] }
	}
	if home == "" {
		home = getenv("ATLAS_HOME")
	}
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".atlas")
		}
	}
	e := environment{
		URL: getenv("ATLAS_URL"), Home: home, Env: getenv("ATLAS_ENV"), Token: getenv("ATLAS_TOKEN"),
		Conformance: getenv("ATLAS_CONFORMANCE") == "1",
	}
	path := filepath.Join(home, "config.toml")
	b, err := readBounded(path, maxConfig)
	if errors.Is(err, fs.ErrNotExist) {
		return e, nil
	}
	if err == nil {
		var cfg config
		if cfg, err = parseConfig(b); err == nil {
			e.ConfigEnv, e.Environments = cfg.Env, cfg.Environments
			return e, nil
		}
	}
	if e.URL == "" && e.Env == "" {
		return e, protocol.NewProblem(protocol.CodeInvalidRequest, path+": "+err.Error()).WithTarget("config.toml").WithNext("Correct config.toml, or name the gateway: atlas --gateway <url>.")
	}
	return e, nil // the gateway is chosen without it
}
