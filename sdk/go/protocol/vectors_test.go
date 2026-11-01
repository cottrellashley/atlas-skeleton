package protocol

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"atlas.local/sdk/go/internal/refs"
	"atlas.local/sdk/go/protocol/ijson"
)

// The registry is §9.3.3 exactly, with §9.7.6's exit codes.
func TestProblemVectors(t *testing.T) {
	var v struct {
		TypeBase string `json:"type_base"`
		Cases    []struct {
			Name     string `json:"name"`
			Code     string `json:"code"`
			Expected struct {
				Status    *int    `json:"status"`
				Retryable *bool   `json:"retryable"`
				Type      *string `json:"type"`
				CLIExit   int     `json:"cli_exit"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(refs.Read(t, "vectors/problems.json"), &v); err != nil {
		t.Fatal(err)
	}
	if v.TypeBase != ProblemTypeBase {
		t.Errorf("type base %q, the vectors' %q", ProblemTypeBase, v.TypeBase)
	}
	registered := 0
	for _, c := range v.Cases {
		if got := ExitCode(c.Code); got != c.Expected.CLIExit {
			t.Errorf("%s: exits %d, the vectors say %d", c.Name, got, c.Expected.CLIExit)
		}
		if c.Expected.Status == nil {
			if Known(c.Code) {
				t.Errorf("%s is in the registry", c.Code)
			}
			continue
		}
		if registered >= len(Registry) || Registry[registered].Code != c.Code {
			t.Errorf("row %d of the registry is not %s", registered, c.Code)
		} else if r := Registry[registered]; r.Status != *c.Expected.Status || r.Retryable != *c.Expected.Retryable || ProblemTypeBase+r.Code != *c.Expected.Type {
			t.Errorf("%s: %+v, the vectors' %d %v %s", c.Code, r, *c.Expected.Status, *c.Expected.Retryable, *c.Expected.Type)
		}
		registered++
	}
	if registered != len(Registry) {
		t.Errorf("%d registered codes, the vectors' %d", len(Registry), registered)
	}
}

// AP-FP-001: every fingerprint vector's canonical form, digest and
// fingerprint.
func TestFingerprintVectors(t *testing.T) {
	doc, err := ijson.Parse(refs.Read(t, "vectors/fingerprints.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases, _ := doc.(*ijson.Object).Get("cases")
	if len(cases.([]ijson.Value)) < 9 {
		t.Fatal("the vectors of §9.4.4 are missing")
	}
	for _, c := range cases.([]ijson.Value) {
		c := c.(*ijson.Object)
		name, _ := c.Get("name")
		profile, _ := c.Get("profile")
		e, _ := c.Get("expected")
		canonical, _ := e.(*ijson.Object).Get("canonical")
		digest, _ := e.(*ijson.Object).Get("sha256")
		want, _ := e.(*ijson.Object).Get("fingerprint")
		before, _ := ijson.Marshal(profile)
		fp, err := Fingerprint(profile.(*ijson.Object))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(fp.Canonical) != canonical.(string) {
			t.Errorf("%s: canonical form\n got %s\nwant %s", name, fp.Canonical, canonical)
		}
		if fp.SHA256 != digest || fp.Value != want {
			t.Errorf("%s: fingerprint %s (%s), want %s (%s)", name, fp.Value, fp.SHA256, want, digest)
		}
		if after, _ := ijson.Marshal(profile); string(after) != string(before) {
			t.Errorf("%s: fingerprinting changed the profile", name)
		}
	}
}

// A client recomputes the fingerprint of every profile it receives (§9.4.2):
// every vector profile carries its own, and a changed contract under it is
// invalid_document.
func TestCheckFingerprint(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join(refs.Dir(t), "vectors", "profiles", "*.json"))
	if len(paths) < 4 {
		t.Fatalf("%d vector profiles", len(paths))
	}
	for _, path := range paths {
		b, _ := os.ReadFile(path)
		v, err := ijson.Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		cases, _ := v.(*ijson.Object).Get("cases")
		for _, c := range cases.([]ijson.Value) {
			p, _ := c.(*ijson.Object).Get("profile")
			profile := p.(*ijson.Object)
			if _, err := CheckFingerprint(profile); err != nil {
				t.Fatalf("%s: %v", filepath.Base(path), err)
			}
			changed := ijson.Clone(profile).(*ijson.Object)
			changed.Set("interactions", ijson.NewObject())
			if _, err := CheckFingerprint(changed); err == nil {
				t.Fatalf("%s: a changed contract under its old fingerprint", filepath.Base(path))
			}
		}
	}
}

// AP-HTTP-001: every Atlas header in the vectors reads exactly as the
// vectors say, or is absent.
func TestHeaderVectors(t *testing.T) {
	var v struct {
		Cases []struct {
			Name     string          `json:"name"`
			Header   string          `json:"header"`
			Lines    []string        `json:"lines"`
			Expected json.RawMessage `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(refs.Read(t, "vectors/headers.json"), &v); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC) // Q-HDR-3's reference year
	for _, c := range v.Cases {
		h := http.Header{}
		for _, l := range c.Lines {
			h.Add(c.Header, l)
		}
		field, _ := Field(h, c.Header)
		got, err := readHeader(c.Header, field, now)
		var e struct {
			Value  any    `json:"value"`
			Absent bool   `json:"absent"`
			Why    string `json:"why"`
		}
		if err := json.Unmarshal(c.Expected, &e); err != nil {
			t.Fatal(err)
		}
		switch {
		case e.Absent && err == nil:
			t.Errorf("%s: read as %v; the vectors say absent (%s)", c.Name, got, e.Why)
		case !e.Absent && err != nil:
			t.Errorf("%s: absent (%v); the vectors say %v", c.Name, err, e.Value)
		case !e.Absent && !reflect.DeepEqual(jsonOf(t, got), jsonOf(t, e.Value)):
			t.Errorf("%s: read as %s; the vectors say %s", c.Name, jsonOf(t, got), jsonOf(t, e.Value))
		}
	}
}

func jsonOf(t *testing.T, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var x any
	_ = json.Unmarshal(b, &x)
	b, _ = json.Marshal(x)
	return string(b)
}

// readHeader is what Atlas reads from each header, in the vectors' form.
func readHeader(name, field string, now time.Time) (any, error) {
	list := func(l []string) []string {
		if l == nil {
			return []string{}
		}
		return l
	}
	switch name {
	case HeaderClient:
		c, err := ParseClient(field)
		return map[string]string{"name": c.Name, "version": c.Version, "protocol": c.Protocol}, err
	case HeaderClientFeatures:
		f, err := ParseFeatures(field)
		return map[string][]string{"levels": list(f.Levels), "lens": list(f.Lens), "deliveries": list(f.Deliveries), "encodings": list(f.Encodings), "traits": list(f.Traits), "handoff": list(f.Handoff)}, err
	case HeaderConfirm:
		d, err := ParseConfirm(field)
		if err != nil {
			return nil, err
		}
		s, err := FormatConfirmDecimal(d)
		return s, err
	case HeaderDeadline:
		d, err := ParseDeadline(field)
		return d.Unix(), err
	case HeaderErrorCode:
		return ParseErrorCode(field)
	case HeaderExecution:
		return ParseExecution(field)
	case HeaderGovernanceRevision:
		return ParseGovernanceRevision(field)
	case HeaderLimitRemaining:
		ls, err := ParseLimits(field)
		out := map[string]int64{}
		for _, l := range ls {
			out[l.Name] = l.Remaining
		}
		return out, err
	case HeaderProfile, HeaderProfileExpect:
		e, err := ParseExactHeader(field)
		return e.String(), err
	case HeaderAuthorization:
		return ParseBearer(field)
	case HeaderCacheControl:
		return ParseCacheControl(field), nil
	case HeaderDeprecation:
		d, err := ParseDeprecation(field)
		return d.Unix(), err
	case HeaderIdempotencyKey:
		return ParseIdempotencyKey(field)
	case HeaderIfMatch:
		return ParseIfMatch(field)
	case HeaderOperationLocation:
		u, err := ParseOperationLocation(field)
		if err != nil {
			return nil, err
		}
		return u.String(), nil
	case HeaderRetryAfter:
		d, err := ParseRetryAfter(field)
		return int64(d / time.Second), err
	case HeaderSunset:
		s, err := ParseSunset(field, now)
		return s.Unix(), err
	case HeaderWWWAuthenticate:
		c, err := ParseChallenge(field)
		params := map[string]string{"realm": c.Realm}
		if c.Error != "" {
			params["error"] = c.Error
		}
		if c.Claims != "" {
			params["claims"] = c.Claims
		}
		return map[string]any{"scheme": "Bearer", "params": params}, err
	case HeaderInteractive:
		return ParseBoolean(field)
	case strings.ToLower(HeaderTraceparent), HeaderTraceparent:
		tp, err := ParseTraceparent(field)
		return map[string]string{"version": tp.Version, "trace_id": tp.TraceID, "parent_id": tp.ParentID, "flags": fmt.Sprintf("%02x", tp.Flags)}, err
	}
	return nil, fmt.Errorf("no reader for %s", name)
}

var _ = strconv.Itoa
