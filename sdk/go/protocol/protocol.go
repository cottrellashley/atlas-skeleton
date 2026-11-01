// Package protocol is atlas/1.0, the wire between an Atlas client and the
// Atlas Gateway (§9), in Go: the version and the words every other package
// speaks (levels, deliveries, encodings, lens operations, executions), the
// media types (§9.1.7), the headers and their Structured Field types (§9.6.3),
// the problem registry (§9.3.3) and fingerprints (§9.4).
//
// The grammars it rests on are its subpackages: ijson (I-JSON and RFC 8785),
// grammar (the ABNF of §9.1) and sf (RFC 9651).
//
// The text of §9 is normative. This package is one implementation of it,
// checked against the vectors in spec/protocol/references, which are written
// from the text and never from this package's output (D22).
package protocol

// Version is the protocol version this package speaks.
const Version = "atlas/1.0"

// WellKnownPath is the bootstrap's path (§9.6.1, RFC 8615).
const WellKnownPath = "/.well-known/atlas"

// Levels a client declares in Atlas-Client-Features (§9). Every client is at
// least Core.
const (
	LevelCore      = "core"
	LevelStreaming = "streaming"
	LevelColumnar  = "columnar"
	LevelBulk      = "bulk"
	LevelEvents    = "events"
	LevelHandoff   = "handoff"
	LevelBroker    = "broker" // reserved until O54 closes; never offered
)

// Deliveries: how an interaction's answer travels (§4.1).
const (
	DeliverySingle       = "single"
	DeliveryChunked      = "chunked"
	DeliverySubscription = "subscription"
	DeliveryOperation    = "operation"
	DeliveryTransfer     = "transfer"
)

// Encodings a binding serves and a client reads (§9.2).
const (
	EncodingJSON    = "json"
	EncodingNDJSON  = "ndjson"
	EncodingArrow   = "arrow"
	EncodingCSV     = "csv"
	EncodingParquet = "parquet"
)

// Lens operations (§9.5.3).
const (
	LensRename      = "rename"
	LensAliasValue  = "alias_value"
	LensConvertUnit = "convert_unit"
	LensPromote     = "promote"
	LensDefault     = "default"
	LensDrop        = "drop"
)

// LensOperations are every lens operation of atlas/1.0, in the order §9.5.3
// lists them.
var LensOperations = []string{LensRename, LensAliasValue, LensConvertUnit, LensPromote, LensDefault, LensDrop}

// Executions of a data result, as Atlas-Execution prefers them (§2.2).
const (
	ExecutionGateway = "gateway"
	ExecutionHandoff = "handoff"
	ExecutionClient  = "client"
)

// Handoff features: the chunk format a client decodes, and whether it can
// reach Snowflake's result storage itself (§2.2.1).
const (
	HandoffSnowflakeArrow1 = "snowflake-arrow/1"
	ReachSnowflake         = "reach-snowflake"
)

// Media types (§9.1.7).
const (
	MediaJSON        = "application/json"
	MediaProblem     = "application/problem+json"
	MediaNDJSON      = "application/x-ndjson"
	MediaArrowStream = "application/vnd.apache.arrow.stream"
	MediaCSV         = "text/csv"
	MediaParquet     = "application/vnd.apache.parquet"
	MediaProfile     = "application/vnd.atlas.profile+json"
	MediaCloudEvent  = "application/cloudevents+json"
	MediaHandoff     = "application/vnd.atlas.handoff+json"
	MediaConnect     = "application/vnd.atlas.connect+json"
)

// MediaFor returns the media type an encoding travels as.
func MediaFor(encoding string) (string, bool) {
	switch encoding {
	case EncodingJSON:
		return MediaJSON, true
	case EncodingNDJSON:
		return MediaNDJSON, true
	case EncodingArrow:
		return MediaArrowStream, true
	case EncodingCSV:
		return MediaCSV, true
	case EncodingParquet:
		return MediaParquet, true
	}
	return "", false
}
