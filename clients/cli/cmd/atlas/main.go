// Command atlas is the Atlas CLI: how a person or an agent explores what
// they may do and tries it (ATLAS-CORE §1, §6). Every command calls the
// same interface the Python and Node SDKs do, with the same request
// records. With --json, or when standard output is not a terminal, output
// is the protocol's documents unchanged (§5.3 rule 2): a record as one JSON
// line, a table as NDJSON rows then its end line, and problems and warnings
// as JSON lines on standard error. The exit code is the problem's (§9.7.6).
//
//	atlas --gateway http://127.0.0.1:8740 login --token dev-alice
//	atlas whoami
//	atlas search "portfolio holdings"
//	atlas guide portfolio.holdings
//	atlas fetch portfolio.holdings --portfolios 147HYC --json
//	atlas call portfolio.holdings@v1:fetch --request '{"portfolios":["147HYC"]}'
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	atlas "github.com/jhgonline/fi-app-atlas/sdk/go"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

const usage = `atlas: the Atlas CLI

  atlas [--gateway URL] [--env NAME] [--json] <command> [arguments]

Commands:
  login --token T [--refresh-token R] [--expires-in D]   sign in to the gateway
  logout                                                 forget this gateway's sign-in
  whoami                                                 who you are, and where
  catalog [--domain D] [--family F] [--effect E]         what you may use
  search WORDS                                           find a capability
  describe ID                                            a capability's contract
  guide ID [--client cli|python|node|go] [--max-tokens N] how to use it
  access [ID] [--domain D]                               why you can or cannot
  fetch ID [--<member> V ...] [--request JSON]           a data fetch, flags from its request
  call ADDRESS [--request JSON|@FILE|-] [--accept M] [--deadline D] [--idempotency-key K]
  version

The gateway is --gateway, else --env's, else ATLAS_URL, else ATLAS_ENV's,
else config.toml's env's (§9.7.2). Sign-in lives under ATLAS_HOME (~/.atlas).
Flags may stand anywhere among the arguments. On a terminal a table is
printed for people; with --json, or into a pipe, a table is NDJSON rows
then its end line, and problems and warnings are JSON on standard error.
The exit code is the problem's: 2 invalid request, 3 unauthenticated,
4 refused, 5 not found, 6 limits and availability, 7 deadline, 8 contract,
1 anything else.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, atlas.Options{}))
}

// cli is one invocation: its streams, its output form, and the options
// its client starts from.
type cli struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	machine        bool // --json, or standard output is not a terminal
	opt            atlas.Options
}

// run is the CLI over its arguments and standard streams; base is what the
// client is connected with (a test gives its HTTP client and home).
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, base atlas.Options) int {
	global := flag.NewFlagSet("atlas", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	gateway := global.String("gateway", "", "the gateway's URL")
	env := global.String("env", "", "an environment's name")
	asJSON := global.Bool("json", false, "the protocol's documents, unchanged")
	rest, err := globals(global, args)
	c := &cli{stdin: stdin, stdout: stdout, stderr: stderr, opt: base}
	c.machine = *asJSON || !isTerminal(stdout)
	if errors.Is(err, flag.ErrHelp) || err == nil && len(rest) == 0 {
		fmt.Fprint(stderr, usage)
		if err != nil {
			return 0
		}
		return 2
	}
	if err != nil {
		return c.fail(flagProblem(err))
	}
	if *gateway != "" {
		c.opt.URL = *gateway
	}
	if *env != "" {
		c.opt.Env = *env
	}
	if c.opt.Name == "" {
		c.opt.Name = "atlas-cli"
	}
	c.opt.Interactive = isTerminal(stdin)
	c.opt.Warn = c.warning
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := c.dispatch(ctx, rest[0], rest[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return c.fail(err)
	}
	return 0
}

// globals takes --gateway, --env and --json wherever they stand, and
// returns the other arguments in their order.
func globals(fs *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(rest, args[i:]...), nil
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") || fs.Lookup(name) == nil && name != "h" && name != "help" {
			rest = append(rest, a)
			continue
		}
		if name == "h" || name == "help" {
			if len(rest) > 0 {
				rest = append(rest, a) // a command's own help
				continue
			}
			return nil, flag.ErrHelp
		}
		set := []string{"--" + name}
		if hasValue {
			set[0] += "=" + value
		} else if name != "json" && i+1 < len(args) {
			i++
			set = append(set, args[i])
		}
		if err := fs.Parse(set); err != nil {
			return nil, err
		}
	}
	return rest, nil
}

func (c *cli) connect(ctx context.Context) (*atlas.Client, error) {
	return atlas.Connect(ctx, c.opt)
}

func (c *cli) dispatch(ctx context.Context, cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	switch cmd {
	case "version":
		if c.machine {
			return c.printJSON(map[string]string{"name": "atlas", "version": atlas.Version, "protocol": protocol.Version})
		}
		fmt.Fprintf(c.stdout, "atlas %s (%s)\n", atlas.Version, protocol.Version)
		return nil
	case "login":
		token := fs.String("token", "", "the access token")
		refresh := fs.String("refresh-token", "", "the refresh token")
		expires := fs.Duration("expires-in", 0, "how long the access token lasts")
		if _, err := parse(fs, args, 0); err != nil {
			return err
		}
		if *token == "" {
			return protocol.NewProblem(protocol.CodeInvalidRequest, "atlas login takes --token: interactive sign-in is the gateway's identity provider's").WithTarget("--token").WithNext("atlas login --token <token>")
		}
		client, err := c.connect(ctx)
		if err != nil {
			return err
		}
		if *refresh == "" && strings.HasPrefix(*token, "dev-") && client.Environment() == "local" {
			*refresh = "refresh-" + *token // the development gateway's refresh tokens
		}
		if err := client.SignIn(*token, *refresh, *expires); err != nil {
			return err
		}
		return c.say(map[string]string{"signed_in": client.Environment(), "gateway": client.Gateway()}, "signed in to %s at %s\n", client.Environment(), client.Gateway())
	case "logout":
		if _, err := parse(fs, args, 0); err != nil {
			return err
		}
		client, err := c.connect(ctx)
		if err != nil {
			return err
		}
		if err := client.SignOut(); err != nil {
			return err
		}
		return c.say(map[string]string{"signed_out": client.Environment()}, "signed out of %s\n", client.Environment())
	case "whoami", "catalog", "search", "describe", "guide", "access":
		return c.discover(ctx, fs, cmd, args)
	case "call":
		return c.call(ctx, fs, args)
	case "fetch":
		return c.fetch(ctx, fs, args)
	}
	return protocol.NewProblem(protocol.CodeInvalidRequest, "atlas has no command "+cmd).WithTarget(cmd).WithNext("atlas --help lists the commands.")
}

func (c *cli) discover(ctx context.Context, fs *flag.FlagSet, cmd string, args []string) error {
	domain := fs.String("domain", "", "only this domain")
	family := fs.String("family", "", "only this family")
	effect := fs.String("effect", "", "only capabilities with an interaction of this effect")
	client := fs.String("client", "cli", "the guide's syntax: cli, python, node or go")
	maxTokens := fs.Int("max-tokens", 0, "the guide's budget")
	want := map[string]int{"whoami": 0, "catalog": 0, "search": -1, "describe": 1, "guide": 1, "access": -2}[cmd]
	pos, err := parse(fs, args, want)
	if err != nil {
		return err
	}
	cl, err := c.connect(ctx)
	if err != nil {
		return err
	}
	switch cmd {
	case "whoami":
		v, err := cl.Whoami(ctx)
		if err != nil {
			return err
		}
		return c.printJSON(v)
	case "describe":
		v, err := cl.Describe(ctx, pos[0])
		if err != nil {
			return err
		}
		return c.printJSON(v)
	case "guide":
		md, err := cl.GuideWithin(ctx, pos[0], *client, *maxTokens)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(c.stdout, md)
		return err
	}
	var items []*ijson.Object
	switch cmd {
	case "catalog":
		filter := map[string]any{}
		for k, v := range map[string]string{"domain": *domain, "family": *family, "effect": *effect} {
			if v != "" {
				filter[k] = v
			}
		}
		items, err = cl.Catalog(ctx, filter)
	case "search":
		items, err = cl.Search(ctx, strings.Join(pos, " "))
	case "access":
		id := ""
		if len(pos) > 0 {
			id = pos[0]
		}
		items, err = cl.AccessIn(ctx, id, *domain)
	}
	if err != nil {
		return err
	}
	return c.list(items)
}

// call is any interaction, by its address, with its request as JSON: the
// CLI's complete interface (ATLAS-CORE §6).
func (c *cli) call(ctx context.Context, fs *flag.FlagSet, args []string) error {
	o := callFlags(fs)
	pos, err := parse(fs, args, 1)
	if err != nil {
		return err
	}
	body, err := readRequest(*o.request, c.stdin)
	if err != nil {
		return err
	}
	cl, err := c.connect(ctx)
	if err != nil {
		return err
	}
	opts, err := o.options()
	if err != nil {
		return err
	}
	r, err := cl.Call(ctx, pos[0], body, opts...)
	if err != nil {
		return err
	}
	return c.result(r)
}

// fetch is a data fetch of an id, its flags built from its request schema
// when the command runs, so a capability deployed a minute ago has them
// (§5.3 rule 1, ATLAS-CLI-002). A bare id is its current version.
func (c *cli) fetch(ctx context.Context, fs *flag.FlagSet, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
			fmt.Fprint(c.stderr, "atlas fetch ID [--<member> V ...] [--request JSON]: flags come from the capability's request schema; atlas fetch ID --help lists them\n")
			return flag.ErrHelp
		}
		return protocol.NewProblem(protocol.CodeInvalidRequest, "atlas fetch takes an id first: atlas fetch <id> [--<member> value ...]").WithTarget("id")
	}
	id := args[0]
	cl, err := c.connect(ctx)
	if err != nil {
		return err
	}
	p, err := cl.Current(ctx, id)
	if err != nil {
		return err
	}
	in := p.Interactions["fetch"]
	if in == nil {
		return protocol.NewProblem(protocol.CodeNotFound, p.Exact().String()+" has no fetch").WithTarget("id").WithNext("atlas describe " + id + " lists its interactions; atlas call reaches any of them.")
	}
	o := callFlags(fs)
	members := requestFlags(fs, p.Schemas[in.Takes])
	if _, err := parse(fs, args[1:], 0); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(c.stderr, members.help(p.Exact().String()))
		}
		return err
	}
	text, err := readRequest(*o.request, c.stdin)
	if err != nil {
		return err
	}
	body, err := members.record(text)
	if err != nil {
		return err
	}
	opts, err := o.options()
	if err != nil {
		return err
	}
	address := p.ID + "@v" + fmt.Sprint(p.Version) + ":fetch"
	r, err := cl.Call(ctx, address, body, opts...)
	if err != nil {
		return err
	}
	return c.result(r)
}

type callOptions struct {
	request, accept, key *string
	deadline             *time.Duration
}

func callFlags(fs *flag.FlagSet) callOptions {
	return callOptions{
		request:  fs.String("request", "", "the request record: JSON, @file, or - for standard input"),
		accept:   fs.String("accept", "", "the answer's encoding: ndjson, json or arrow"),
		deadline: fs.Duration("deadline", 0, "the call's deadline"),
		key:      fs.String("idempotency-key", "", "the change's key, for a call whose effect is not read"),
	}
}

func (o callOptions) options() ([]atlas.CallOption, error) {
	var opts []atlas.CallOption
	if *o.deadline > 0 {
		opts = append(opts, atlas.WithDeadline(*o.deadline))
	}
	if *o.accept != "" {
		m, ok := protocol.MediaFor(*o.accept)
		if !ok {
			return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "no encoding "+*o.accept).WithTarget("--accept").WithNext("--accept ndjson, json or arrow.")
		}
		opts = append(opts, atlas.WithAccept(m))
	}
	if *o.key != "" {
		opts = append(opts, atlas.WithIdempotencyKey(*o.key))
	}
	return opts, nil
}

// maxRequest bounds a request read from a file or standard input.
const maxRequest = 16 << 20

func readRequest(arg string, stdin io.Reader) (string, error) {
	var b []byte
	var err error
	switch {
	case arg == "":
		return "{}", nil
	case arg == "-":
		b, err = io.ReadAll(io.LimitReader(stdin, maxRequest+1))
	case strings.HasPrefix(arg, "@"):
		var f *os.File
		if f, err = os.Open(arg[1:]); err == nil {
			b, err = io.ReadAll(io.LimitReader(f, maxRequest+1))
			f.Close()
		}
	default:
		return arg, nil
	}
	if err != nil {
		return "", protocol.NewProblem(protocol.CodeInvalidRequest, "the request cannot be read: "+err.Error()).WithTarget("--request")
	}
	if len(b) > maxRequest {
		return "", protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("the request is larger than %d bytes", maxRequest)).WithTarget("--request")
	}
	return string(b), nil
}

func isTerminal(f any) bool {
	file, ok := f.(*os.File)
	if !ok {
		return false
	}
	st, err := file.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
