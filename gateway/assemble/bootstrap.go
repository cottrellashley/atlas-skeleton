package assemble

import (
	"errors"
	"fmt"
	"strings"

	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/documents"
	"atlas.local/sdk/go/protocol/ijson"
)

// checkBootstrap refuses parts whose bootstrap would fail its schema (§9.3
// rule 1, AP-DOC-001, R125): the members a deployment's settings decide
// (the environment, the gateway's URL, how to sign in), written as the edge
// writes them, are validated against bootstrap.schema.json, and every
// failure is named. A gateway every client would refuse never starts.
func checkBootstrap(p Parts) error {
	scopes := make([]ijson.Value, len(p.SignIn.Scopes))
	for i, s := range p.SignIn.Scopes {
		scopes[i] = s
	}
	signIn := ijson.NewObject()
	signIn.Set("authority", p.SignIn.Authority)
	signIn.Set("client_id", p.SignIn.ClientID)
	signIn.Set("scopes", scopes)
	signIn.Set("token_endpoint", p.SignIn.TokenEndpoint)
	if p.SignIn.AuthorizationEndpoint != "" {
		signIn.Set("authorization_endpoint", p.SignIn.AuthorizationEndpoint)
	}
	if p.SignIn.DeviceAuthorizationEndpoint != "" {
		signIn.Set("device_authorization_endpoint", p.SignIn.DeviceAuthorizationEndpoint)
	}
	server := ijson.NewObject()
	server.Set("url", strings.TrimRight(p.URL, "/")) // as edge.New keeps it
	servers := ijson.NewObject()
	servers.Set("gateway", server)
	doc := ijson.NewObject()
	doc.Set("protocols", []ijson.Value{protocol.Version})
	doc.Set("minimum_protocol", protocol.Version)
	doc.Set("environment", p.Environment)
	doc.Set("servers", servers)
	doc.Set("builtins", ijson.NewObject())
	doc.Set("sign_in", signIn)
	doc.Set("catalog_etag", `"0"`)
	failures := documents.Validate("bootstrap", doc)
	if len(failures) == 0 {
		return nil
	}
	errs := make([]error, len(failures))
	for i, f := range failures {
		errs[i] = fmt.Errorf("%s: %s (%s)", f.Pointer, f.Message, f.Keyword)
	}
	return fmt.Errorf("assemble: the bootstrap these settings make fails its schema, so no client would accept it: %w", errors.Join(errs...))
}
