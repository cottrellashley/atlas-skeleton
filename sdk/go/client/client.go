// Package client is the Go client of the Atlas Gateway: the reader side of
// every contract (§9.7). It holds no code for any capability. It finds the
// gateway, learns each capability from its profile, validates requests,
// follows the bindings it is served, and resolves every answer to the
// profile its caller knows: the one its lock names, or the current one. The
// CLI, Go programs and the Python SDK's engine all call through it.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/snowarrow"
)

// Name and Version identify this client in Atlas-Client.
const (
	Name    = "atlas-go"
	Version = "0.1.0"
)

// Features is what this client implements and declares on every call.
var Features = protocol.Features{
	Levels:  []string{protocol.LevelCore, protocol.LevelStreaming, protocol.LevelColumnar, protocol.LevelHandoff},
	Handoff: []string{snowarrow.Format},
}

// Options configure a client. Empty members fall back to the environment
// (§9.7.2): ATLAS_URL and ATLAS_TOKEN.
type Options struct {
	URL   string
	Token string
	// Lock pins the reader profiles; with none, the client reads under
	// whatever profile is current.
	Lock     *Lock
	HTTP     *http.Client
	Mem      memory.Allocator
	Parallel int // chunk downloads at a time; 8 when zero
}

// Client calls one gateway.
type Client struct {
	token    string
	lock     *Lock
	http     *http.Client
	mem      memory.Allocator
	parallel int
	boot     protocol.Bootstrap

	mu       sync.Mutex
	profiles map[string]*protocol.Profile
}

// Connect reads the gateway's bootstrap: the only address a client knows in
// advance (§2.6).
func Connect(ctx context.Context, o Options) (*Client, error) {
	url := strings.TrimRight(first(o.URL, os.Getenv("ATLAS_URL")), "/")
	if url == "" {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "no gateway: pass a URL or set ATLAS_URL")
	}
	c := &Client{
		token: first(o.Token, os.Getenv("ATLAS_TOKEN")), lock: o.Lock, http: o.HTTP, mem: o.Mem,
		parallel: o.Parallel, profiles: map[string]*protocol.Profile{},
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	if c.mem == nil {
		c.mem = memory.NewGoAllocator()
	}
	if c.parallel == 0 {
		c.parallel = 8
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/.well-known/atlas", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeUnavailable, "the gateway at "+url+" is not reachable: "+err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&c.boot) != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, url+" did not answer with an Atlas bootstrap")
	}
	if !strings.HasPrefix(c.boot.Protocol, "atlas/0.") {
		return nil, protocol.NewProblem(protocol.CodeUpgradeRequired, "the gateway speaks "+c.boot.Protocol+"; this client speaks "+protocol.Version)
	}
	return c, nil
}

// Bootstrap is what the gateway said about itself.
func (c *Client) Bootstrap() protocol.Bootstrap { return c.boot }

// Lock is the client's lock, if any.
func (c *Client) Lock() *Lock { return c.lock }

// post sends one call to a binding. A failure comes back as the gateway's
// problem; the client never builds an address, it follows the binding.
func (c *Client) post(ctx context.Context, b protocol.Binding, body any, headers map[string]string) (*http.Response, error) {
	server, ok := c.boot.Servers[b.Server]
	if !ok {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "a binding names the server "+b.Server+", which the bootstrap does not")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+b.Address, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", protocol.MediaJSON)
	req.Header.Set(protocol.HeaderClient, fmt.Sprintf("name=%q, version=%q, protocol=%q", Name, Version, protocol.Version))
	req.Header.Set(protocol.HeaderClientFeatures, Features.String())
	req.Header.Set(protocol.HeaderDeadline, protocol.FormatDeadline(deadline))
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeUnavailable, "the gateway did not answer: "+err.Error())
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, readProblem(resp)
	}
	return resp, nil
}

func readProblem(resp *http.Response) error {
	var p protocol.Problem
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if json.Unmarshal(b, &p) != nil || p.Code == "" {
		// A code the client cannot read is handled by its status (§9.3.3).
		p = *protocol.NewProblem(protocol.CodeUpstreamFailed, fmt.Sprintf("the gateway answered %s without a problem document", resp.Status))
		p.Status = resp.StatusCode
	}
	return &p
}

// builtin calls an atlas.* capability at the binding the bootstrap names.
func (c *Client) builtin(ctx context.Context, address string, req, out any) error {
	b, ok := c.boot.Builtins[address]
	if !ok {
		return protocol.NewProblem(protocol.CodeNotFound, "the gateway does not serve "+address)
	}
	resp, err := c.post(ctx, b, req, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return protocol.NewProblem(protocol.CodeInvalidDocument, address+": "+err.Error())
	}
	return nil
}

// WhoAmI asks who the caller is to the gateway.
func (c *Client) WhoAmI(ctx context.Context) (protocol.WhoAmI, error) {
	var out protocol.WhoAmI
	return out, c.builtin(ctx, protocol.AddressWhoAmI, struct{}{}, &out)
}

// Catalog is the working view: only what the caller can use.
func (c *Client) Catalog(ctx context.Context) (protocol.CatalogPage, error) {
	var out protocol.CatalogPage
	return out, c.builtin(ctx, protocol.AddressCatalog, struct{}{}, &out)
}

// Search ranks the working view against words.
func (c *Client) Search(ctx context.Context, words string) (protocol.CatalogPage, error) {
	var out protocol.CatalogPage
	return out, c.builtin(ctx, protocol.AddressSearch, protocol.SearchRequest{Words: words}, &out)
}

// Describe returns a capability's current profile (id@vN, or a bare id at a
// terminal) or one exact profile (id@vN#fingerprint). Every profile's
// fingerprint is recomputed and a mismatch refused (§9.7.5).
func (c *Client) Describe(ctx context.Context, ref string) (*protocol.DescribeAnswer, error) {
	req := protocol.DescribeRequest{ID: ref}
	if strings.Contains(ref, "#") {
		req = protocol.DescribeRequest{Exact: ref}
	}
	var ans protocol.DescribeAnswer
	if err := c.builtin(ctx, protocol.AddressDescribe, req, &ans); err != nil {
		return nil, err
	}
	e, err := protocol.ExactOf(&ans.Profile)
	if err != nil || e.String() != ans.Exact {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, fmt.Sprintf("the profile served as %s has the fingerprint %s", ans.Exact, e.Fingerprint))
	}
	c.mu.Lock()
	c.profiles[ans.Exact] = &ans.Profile
	c.mu.Unlock()
	return &ans, nil
}

// Profile returns one exact profile, cached for ever: a fingerprint's
// profile never changes.
func (c *Client) Profile(ctx context.Context, exact string) (*protocol.Profile, error) {
	c.mu.Lock()
	p, ok := c.profiles[exact]
	c.mu.Unlock()
	if ok {
		return p, nil
	}
	ans, err := c.Describe(ctx, exact)
	if err != nil {
		return nil, err
	}
	return &ans.Profile, nil
}

func (c *Client) source(ctx context.Context) protocol.ProfileSource {
	return func(exact string) (*protocol.Profile, error) { return c.Profile(ctx, exact) }
}

// reader is the profile a caller reads a capability version under: its
// lock's entry, else the current one.
func (c *Client) reader(ctx context.Context, p protocol.Pinned) (string, *protocol.Profile, error) {
	if c.lock != nil {
		if exact, ok := c.lock.Capabilities[p.String()]; ok {
			prof, err := c.Profile(ctx, exact)
			return exact, prof, err
		}
	}
	ans, err := c.Describe(ctx, p.String())
	if err != nil {
		return "", nil, err
	}
	return ans.Exact, &ans.Profile, nil
}

// CallOptions are what the client sends as headers, never as members of the
// request: the request record is the same in every client (AP-CLIENT-003).
type CallOptions struct {
	// Execution is a preference: gateway or handoff.
	Execution string
}

// Fetch calls a table interaction and returns its rows, resolved to the
// reader's profile (§9.7.4). The caller releases the table.
func (c *Client) Fetch(ctx context.Context, address string, request map[string]any, o CallOptions) (*Table, error) {
	addr, err := protocol.ParseAddress(address)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, err.Error())
	}
	readerExact, reader, err := c.reader(ctx, addr.Pinned)
	if err != nil {
		return nil, err
	}
	inter, ok := reader.Interactions[addr.Interaction]
	if !ok || reader.Result == nil {
		return nil, protocol.NewProblem(protocol.CodeNotFound, fmt.Sprintf("%s has no table interaction %s", addr.Pinned, addr.Interaction))
	}
	req, err := c.validate(reader, request)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{protocol.HeaderProfileExpect: readerExact, "Accept": protocol.MediaArrowStream}
	if o.Execution != "" {
		headers[protocol.HeaderExecution] = o.Execution
	}
	resp, err := c.post(ctx, inter.Bindings[0], req, headers)
	if err != nil {
		return nil, err
	}
	writerExact := resp.Header.Get(protocol.HeaderProfile)
	writer, err := c.Profile(ctx, writerExact)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	chain, err := protocol.BuildChain(c.source(ctx), writerExact, readerExact)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	readerTable := reader.Result.Project(columnNames(req["columns"]))
	readerSchema, err := protocol.ArrowSchema(readerTable, readerExact)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	t := &Table{Writer: writerExact, Reader: readerExact}
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch media {
	case protocol.MediaArrowStream:
		ipcr, err := ipc.NewReader(resp.Body, ipc.WithAllocator(c.mem))
		if err != nil {
			resp.Body.Close()
			return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "the answer is not an Arrow stream: "+err.Error())
		}
		t.Execution = protocol.ExecutionGateway
		t.RecordReader = newResolving(c.mem, &arrowAnswer{ipcr: ipcr, body: resp.Body}, chain, writer, writerExact, readerTable, readerSchema)
	case protocol.MediaHandoff:
		var h protocol.Handoff
		err := json.NewDecoder(resp.Body).Decode(&h)
		resp.Body.Close()
		if err != nil {
			return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "the handoff document: "+err.Error())
		}
		src, err := c.handoff(ctx, inter.Bindings[0], req, readerExact, writer, &h)
		if err != nil {
			return nil, err
		}
		t.Execution = protocol.ExecutionHandoff
		t.RecordReader = newResolving(c.mem, src, chain, writer, writerExact, readerTable, readerSchema)
	default:
		resp.Body.Close()
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "the gateway answered "+media+", which this client did not ask for")
	}
	return t, nil
}

// Call calls an interaction whose answer is one record, such as explain.
func (c *Client) Call(ctx context.Context, address string, request map[string]any) (json.RawMessage, error) {
	addr, err := protocol.ParseAddress(address)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, err.Error())
	}
	readerExact, reader, err := c.reader(ctx, addr.Pinned)
	if err != nil {
		return nil, err
	}
	inter, ok := reader.Interactions[addr.Interaction]
	if !ok {
		return nil, protocol.NewProblem(protocol.CodeNotFound, fmt.Sprintf("%s has no interaction %s", addr.Pinned, addr.Interaction))
	}
	req, err := c.validate(reader, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.post(ctx, inter.Bindings[0], req, map[string]string{protocol.HeaderProfileExpect: readerExact})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// validate checks the request against the reader's schema before anything is
// sent (ATLAS-TYPES-003); an invalid request never leaves the client.
func (c *Client) validate(reader *protocol.Profile, request map[string]any) (map[string]any, error) {
	if request == nil {
		request = map[string]any{}
	}
	b, err := json.Marshal(request)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, err.Error())
	}
	req, err := protocol.DecodeRecord(b)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, err.Error())
	}
	if failures := protocol.Validate(reader.Request, req); len(failures) > 0 {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, failures[0].Message).WithTarget(failures[0].Pointer)
	}
	return req, nil
}

func columnNames(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
