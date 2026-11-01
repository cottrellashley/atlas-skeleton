// Package support is Acme's support desk in Atlas: support.tickets, served
// by a provider over the desk's HTTP API and scoped by Acme's workspace
// dimension. It is a company integration written against Atlas's public
// interface alone (HANDOFF.md C1): a ports.Module, its profile, and the
// dimension it defines. It changes nothing in Atlas.
package support

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

//go:embed profiles
var profiles embed.FS

// Workspace is the access dimension Acme's policy scopes tickets by.
var Workspace = ports.Dimension{Name: "workspace", Description: "An Acme support workspace, by its code."}

// MaxAnswer is the most bytes the provider reads of one desk answer.
const MaxAnswer = 8 << 20

// Config is how the module reaches the desk.
type Config struct {
	// Desk is the desk API's base URL.
	Desk string
	// HTTP is the client it is reached with; its own timeout, if any, is
	// within every call's deadline, which each request carries.
	HTTP *http.Client
	// Concurrency is how many calls may reach the desk at once.
	Concurrency int
}

// Module is the support desk's module.
func Module(c Config) (ports.Module, error) {
	base, err := url.Parse(c.Desk)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return ports.Module{}, fmt.Errorf("support: the desk's URL %q is not an absolute URL", c.Desk)
	}
	if c.HTTP == nil {
		c.HTTP = http.DefaultClient
	}
	doc, err := profiles.ReadFile("profiles/support.tickets@v1.json")
	if err != nil {
		return ports.Module{}, fmt.Errorf("support: %w", err)
	}
	p := &provider{desk: base, http: c.HTTP}
	return ports.Module{
		Name:         "support-desk",
		Capabilities: []ports.Capability{{Name: "support.tickets@v1", Profile: doc}},
		Provider:     p,
		Concurrency:  c.Concurrency,
		Health:       p.health,
	}, nil
}

type provider struct {
	desk *url.URL
	http *http.Client
}

// Serve answers support.tickets@v1:fetch. It narrows the desk's read to
// the workspaces the request names, which the pipeline has already held
// to the caller's scope, or else to the caller's scope itself; the
// pipeline still holds every row it returns to that scope.
func (p *provider) Serve(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	if inv.Interaction != "fetch" {
		return nil, fmt.Errorf("support: no interaction %q", inv.Interaction)
	}
	var workspaces []string
	if v, ok := inv.Request.Get("workspaces"); ok && v != nil {
		list, isList := v.([]ijson.Value)
		if !isList {
			return nil, errors.New("support: workspaces is not a list")
		}
		for _, w := range list {
			code, isString := w.(string)
			if !isString {
				return nil, errors.New("support: a workspace is not a code")
			}
			workspaces = append(workspaces, code)
		}
	} else if !inv.Scope.Open(Workspace.Name) {
		workspaces = slices.Clone(inv.Scope[Workspace.Name])
		if len(workspaces) == 0 {
			return &ports.Answer{Rows: ports.SliceRows(nil)}, nil // nothing to read
		}
	}
	q := url.Values{}
	for _, w := range workspaces {
		q.Add("workspace", w)
	}
	if v, ok := inv.Request.Get("status"); ok && v != nil {
		status, isString := v.(string)
		if !isString {
			return nil, errors.New("support: status is not a string")
		}
		q.Set("status", status)
	}
	if inv.MaxRows > 0 {
		q.Set("limit", strconv.Itoa(inv.MaxRows+1)) // one past the cap, so a truncation shows
	}
	u := p.desk.JoinPath("tickets")
	u.RawQuery = q.Encode()
	var got struct {
		Tickets []struct {
			ID        string `json:"id"`
			Workspace string `json:"workspace"`
			Title     string `json:"title"`
			Status    string `json:"status"`
			OpenedAt  string `json:"opened_at"`
		} `json:"tickets"`
	}
	if err := p.get(ctx, u, &got); err != nil {
		return nil, err
	}
	rows := make([]*ijson.Object, 0, len(got.Tickets))
	for _, t := range got.Tickets {
		o := ijson.NewObject()
		o.Set("id", t.ID)
		o.Set("workspace", t.Workspace)
		o.Set("title", t.Title)
		o.Set("status", t.Status)
		o.Set("opened_at", t.OpenedAt)
		rows = append(rows, o)
	}
	return &ports.Answer{Rows: ports.SliceRows(rows)}, nil
}

// get reads one desk answer into v, within ctx's deadline and MaxAnswer. A
// desk that fails or cannot be reached is desk_unavailable; a call whose
// context ended is that context's error, which the pipeline reports.
func (p *provider) get(ctx context.Context, u *url.URL, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("support: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	res, err := p.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("support: the desk: %w", ctx.Err())
		}
		return unavailable(fmt.Sprintf("the support desk cannot be reached: %v", err))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		return unavailable(fmt.Sprintf("the support desk answered %d", res.StatusCode))
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, MaxAnswer+1))
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("support: the desk: %w", ctx.Err())
		}
		return fmt.Errorf("support: the desk's answer was cut off: %w", unavailable(err.Error()))
	}
	if len(b) > MaxAnswer {
		return errors.New("support: the desk's answer is larger than MaxAnswer")
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("support: the desk's answer: %w", err)
	}
	return nil
}

// health asks the desk whether it answers.
func (p *provider) health(ctx context.Context) error {
	var v struct{}
	return p.get(ctx, p.desk.JoinPath("health"), &v)
}

// deskUnavailable is support.tickets' declared error, as its profile
// declares it.
var deskUnavailable = protocol.Declared{Code: "desk_unavailable", Status: 503, Retryable: true,
	Description: "The support desk did not answer, or answered with a failure.", Next: "Wait retry_after seconds, then repeat the call."}

func unavailable(detail string) *protocol.Problem {
	return deskUnavailable.Problem(detail).WithRetryAfter(5)
}
