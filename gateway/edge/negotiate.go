package edge

import (
	"net/http"
	"slices"
	"strings"

	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/profile"
)

// The answer's encoding (§9.6.4, §9.C). It is the first media type Accept
// lists that the call's binding serves and the edge can frame for the
// interaction's delivery. A client that lacks the level an encoding needs
// is served the next way down, never refused: Arrow becomes NDJSON, NDJSON
// a JSON array (§9.C). No Accept, or */*, takes the first the binding lists
// that the client reads (A-005, A-152). Accept is read whole, every field
// line of it, media ranges included; parameters and q are ignored (§9.6.4).
// The edge negotiates only once the pipeline has decided the call's access,
// so a caller who may not know a capability learns nothing of its binding.

// offer is one encoding a call may be answered in.
type offer struct {
	encoding string // json, ndjson, arrow
	media    string
}

// down is the next way down from an encoding (§9.C).
var down = map[string]string{
	protocol.EncodingArrow:  protocol.EncodingNDJSON,
	protocol.EncodingNDJSON: protocol.EncodingJSON,
}

// offers are the encodings the call's binding lists that the edge frames
// for the interaction's delivery and result, in the binding's order.
func offers(in *profile.Interaction, address string) []offer {
	var out []offer
	for _, b := range in.Bindings {
		if b.Protocol != "https" || b.Address != address {
			continue
		}
		for _, e := range b.Encodings {
			if !frames(in, e) || slices.ContainsFunc(out, func(o offer) bool { return o.encoding == e }) {
				continue
			}
			m, _ := protocol.MediaFor(e)
			out = append(out, offer{encoding: e, media: m})
		}
	}
	return out
}

// frames reports whether the edge frames an encoding for an interaction:
// a single answer as JSON, a chunked table as NDJSON, Arrow or a JSON
// array (the references' conformance.rows@v1, Q-SCN-3). Every other
// delivery is not served by this gateway yet (handoff and events are
// deferred, R10, R16), nor csv or parquet.
func frames(in *profile.Interaction, encoding string) bool {
	switch in.Delivery {
	case protocol.DeliverySingle, "":
		return encoding == protocol.EncodingJSON
	case protocol.DeliveryChunked:
		switch encoding {
		case protocol.EncodingJSON:
			return true
		case protocol.EncodingNDJSON, protocol.EncodingArrow:
			return in.Result == "table"
		}
	}
	return false
}

// served reports whether the edge serves an interaction's delivery at all.
func served(in *profile.Interaction) bool {
	switch in.Delivery {
	case protocol.DeliverySingle, protocol.DeliveryChunked, "":
		return true
	}
	return false
}

// reads reports whether a client reads an encoding by the levels it
// declares (§9.C): every client reads JSON; NDJSON needs Streaming and
// Arrow Columnar. A client that lists its encodings is held to that list
// as well (A-152).
func reads(f protocol.Features, encoding string) bool {
	switch encoding {
	case protocol.EncodingJSON:
		return true
	case protocol.EncodingNDJSON:
		return f.Has(protocol.LevelStreaming) && (f.Encodings == nil || f.Reads(encoding))
	case protocol.EncodingArrow:
		return f.Has(protocol.LevelColumnar) && (f.Encodings == nil || f.Reads(encoding))
	}
	return false
}

// mediaRange is one element of Accept: a type and a subtype, either *.
type mediaRange struct{ typ, sub string }

func (m mediaRange) matches(media string) bool {
	typ, sub, _ := strings.Cut(media, "/")
	return m.typ == "*" || m.typ == typ && (m.sub == "*" || m.sub == sub)
}

// maxRanges bounds how many media ranges of Accept are read.
const maxRanges = 32

// accepts reads Accept's field lines (RFC 9110 §12.5.1): its media ranges
// in order, lowercased, parameters and q dropped. An element that is not a
// media range is skipped. sent reports whether Accept had any content.
func accepts(h http.Header) (out []mediaRange, sent bool) {
	for _, line := range h.Values(protocol.HeaderAccept) {
		for _, el := range splitList(line) {
			sent = true
			if len(out) == maxRanges {
				return out, true
			}
			mt, _, _ := strings.Cut(el, ";")
			typ, sub, ok := strings.Cut(strings.ToLower(strings.TrimSpace(mt)), "/")
			if !ok || !token(typ) || !token(sub) || typ == "*" && sub != "*" {
				continue
			}
			out = append(out, mediaRange{typ, sub})
		}
	}
	return out, sent
}

// splitList splits a field line on the commas outside quoted strings, and
// drops the empty elements.
func splitList(line string) []string {
	var out []string
	quoted, escaped, start := false, false, 0
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case escaped:
			escaped = false
		case quoted && c == '\\':
			escaped = true
		case c == '"':
			quoted = !quoted
		case c == ',' && !quoted:
			if el := strings.TrimSpace(line[start:i]); el != "" {
				out = append(out, el)
			}
			start = i + 1
		}
	}
	if el := strings.TrimSpace(line[start:]); el != "" {
		out = append(out, el)
	}
	return out
}

// token reports whether s is an RFC 9110 token.
func token(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0) {
			return false
		}
	}
	return true
}

// choice is how a call is answered: its encoding, and the streaming ones
// the client could have asked for instead, which a cut JSON answer names.
type choice struct {
	offer
	streams []offer
}

// choose picks a call's encoding, or says why there is none.
func choose(h http.Header, f protocol.Features, in *profile.Interaction, address string) (choice, *protocol.Problem) {
	if !served(in) {
		return choice{}, protocol.NewProblem(protocol.CodeNotInEnvironment, "this gateway does not serve "+in.Delivery+" delivery yet").WithNext("Call atlas.describe@v1 for the capability's other interactions.")
	}
	offs := offers(in, address)
	var readable []string
	var c choice
	for _, o := range offs {
		if reads(f, o.encoding) {
			readable = append(readable, o.media)
			if o.encoding != protocol.EncodingJSON {
				c.streams = append(c.streams, o)
			}
		}
	}
	if len(readable) == 0 {
		return choice{}, protocol.NewProblem(protocol.CodeInternal, "the capability's binding lists no encoding this gateway frames for its delivery")
	}
	rs, sent := accepts(h)
	if !sent {
		rs = []mediaRange{{"*", "*"}}
	}
	for _, mr := range rs {
		for _, o := range offs {
			if !mr.matches(o.media) {
				continue
			}
			for e := o.encoding; e != ""; e = down[e] {
				if i := slices.IndexFunc(offs, func(x offer) bool { return x.encoding == e }); i >= 0 && reads(f, e) {
					c.offer = offs[i]
					return c, nil
				}
			}
		}
	}
	list := strings.Join(readable, ", ")
	p := protocol.NewProblem(protocol.CodeInvalidRequest, "the binding serves none of the media types Accept lists; it serves "+list).WithTarget(protocol.HeaderAccept).WithNext("Ask for one of " + list + ".")
	p.Status = http.StatusNotAcceptable
	return choice{}, p
}
