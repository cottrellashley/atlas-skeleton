package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"atlas.local/gateway/core/registry"
	"atlas.local/sdk/go/kit/health"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
)

// bootstrapDoc is the bootstrap as one snapshot makes it: built once per
// snapshot, so the one unauthenticated document costs a flood nothing but
// its bytes.
type bootstrapDoc struct {
	snap *registry.Snapshot
	body []byte
	etag string
}

// bootstrap is GET /.well-known/atlas (§9.6.1): no sign-in, a strong ETag,
// revalidated with If-None-Match. It is the one answer a client may keep,
// and it is no-cache: kept, but revalidated on every use (D3, A-153).
func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	doc, err := s.bootstrapDoc()
	if err != nil {
		s.problem(w, r, err)
		return
	}
	h := w.Header()
	h.Set(protocol.HeaderETag, doc.etag)
	h.Set(protocol.HeaderCacheControl, "no-cache")
	if protocol.IfNoneMatch(r.Header.Get(protocol.HeaderIfNoneMatch), doc.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set(protocol.HeaderContentType, protocol.MediaJSON)
	if _, err := w.Write(doc.body); err != nil {
		exchangeOf(r).gone = true
	}
}

func (s *Server) bootstrapDoc() (*bootstrapDoc, error) {
	snap := s.opt.Pipeline.Registry().Snapshot()
	if d := s.boot.Load(); d != nil && d.snap == snap {
		return d, nil
	}
	builtins := ijson.NewObject()
	for _, c := range snap.Capabilities() {
		if !c.Builtin() {
			continue
		}
		interactions := ijson.NewObject()
		doc, _ := c.Current.Doc.Get("interactions")
		all, ok := doc.(*ijson.Object)
		if !ok {
			return nil, fmt.Errorf("edge: the built-in %s has no interactions", c.Pinned)
		}
		for _, name := range c.Current.Order {
			in, _ := all.Get(name)
			one, ok := in.(*ijson.Object)
			if !ok {
				return nil, fmt.Errorf("edge: the built-in %s has no interaction %s", c.Pinned, name)
			}
			bindings, _ := one.Get("bindings")
			o := ijson.NewObject()
			o.Set("bindings", ijson.Clone(bindings))
			interactions.Set(name, o)
		}
		b := ijson.NewObject()
		b.Set("version", ijson.Number(strconv.Itoa(c.Pinned.Version)))
		b.Set("interactions", interactions)
		builtins.Set(c.Pinned.ID, b)
	}
	scopes := make([]ijson.Value, len(s.opt.SignIn.Scopes))
	for i, sc := range s.opt.SignIn.Scopes {
		scopes[i] = sc
	}
	signIn := ijson.NewObject()
	signIn.Set("authority", s.opt.SignIn.Authority)
	signIn.Set("client_id", s.opt.SignIn.ClientID)
	signIn.Set("scopes", scopes)
	signIn.Set("token_endpoint", s.opt.SignIn.TokenEndpoint)
	if s.opt.SignIn.AuthorizationEndpoint != "" {
		signIn.Set("authorization_endpoint", s.opt.SignIn.AuthorizationEndpoint)
	}
	if s.opt.SignIn.DeviceAuthorizationEndpoint != "" {
		signIn.Set("device_authorization_endpoint", s.opt.SignIn.DeviceAuthorizationEndpoint)
	}
	server := ijson.NewObject()
	server.Set("url", s.opt.URL)
	server.Set("description", "The Atlas gateway.")
	servers := ijson.NewObject()
	servers.Set("gateway", server)
	doc := ijson.NewObject()
	doc.Set("protocols", []ijson.Value{protocol.Version})
	doc.Set("minimum_protocol", protocol.Version)
	doc.Set("environment", s.opt.Pipeline.Environment())
	doc.Set("servers", servers)
	doc.Set("builtins", builtins)
	doc.Set("sign_in", signIn)
	doc.Set("catalog_etag", snap.ETag)
	body, err := ijson.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("edge: the bootstrap: %w", err)
	}
	sum := sha256.Sum256(body)
	d := &bootstrapDoc{snap: snap, body: body, etag: protocol.StrongETag(hex.EncodeToString(sum[:16]))}
	s.boot.Store(d)
	return d, nil
}

// readiness is GET /readyz (§6.7.15): ready when the process is (not
// starting, not draining) and a catalogue snapshot is loaded, and every
// further check the deployment names passes, each within 2 s. It names
// every check it ran and its result (ATLAS-MECH-015), and withholds the
// reasons, which go to the log.
func (s *Server) readiness() http.Handler {
	var checks []health.Check
	if s.opt.Ready != nil {
		checks = append(checks, health.ReadinessCheck(s.opt.Ready))
	}
	checks = append(checks, health.NamedCheck("snapshot", func(context.Context) error {
		if s.opt.Pipeline.Registry().Snapshot().Generation == 0 {
			return errors.New("edge: no catalogue snapshot is loaded")
		}
		return nil
	}))
	for _, c := range s.opt.Checks {
		checks = append(checks, health.WithTimeout(c, readyCheckTimeout))
	}
	h := health.Handler("readyz", s.opt.Log, checks...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every check, named, whatever the query asks: a probe's verdict is
		// the replica's whole state.
		r = r.Clone(r.Context())
		r.URL.RawQuery = "verbose"
		h.ServeHTTP(w, r)
	})
}
