// Package edgehttp is the HTTP edge (§9.6, layer L5): a thin adapter that
// turns HTTP requests into pipeline calls and pipeline outcomes into HTTP
// answers. It decides nothing. Transport concerns are its middleware, in the
// order of §6.7.2; everything else is the pipeline's.
package edgehttp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/middleware"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/problem"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/telemetry"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// MaxBody is the largest request body; uploads go to transfer links instead
// (ATLAS-MECH-002).
const MaxBody = 1 << 20

// Edge serves the Atlas Protocol over HTTP.
type Edge struct {
	Pipeline  *pipeline.Pipeline
	Bootstrap protocol.Bootstrap
	Mem       memory.Allocator
	Log       *slog.Logger
}

// Handler is the edge's routes behind its middleware.
func (e *Edge) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/atlas", e.bootstrap)
	mux.HandleFunc("POST /v1/atlas/{address}", e.call)
	mux.HandleFunc("POST /v1/data/{address}", e.call)
	return middleware.Chain(mux,
		middleware.Recover(e.Log),
		e.logger,
		middleware.RequestID(protocol.HeaderRequestID),
		telemetry.Propagate(false),
		telemetry.AccessLog(e.Log, telemetry.AccessLogOptions{}),
		middleware.MaxBytes(MaxBody),
		middleware.Deadline(protocol.HeaderDeadline, pipeline.DefaultDeadline, 5*time.Minute),
	)
}

// logger gives every request the edge's logger, which the access log and
// every handler log through.
func (e *Edge) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(telemetry.WithLogger(r.Context(), e.Log)))
	})
}

// bootstrap is the one address a client knows in advance; it needs no
// sign-in (ATLAS-DISCOVERY-001).
func (e *Edge) bootstrap(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, protocol.MediaJSON, e.Bootstrap)
}

func (e *Edge) call(w http.ResponseWriter, r *http.Request) {
	rid := middleware.RequestIDFrom(r.Context())
	body, err := io.ReadAll(r.Body)
	if err != nil {
		problem.Write(w, rid, err)
		return
	}
	out, err := e.Pipeline.Handle(r.Context(), pipeline.Request{
		Token:     bearer(r),
		Address:   r.PathValue("address"),
		Body:      body,
		Expect:    r.Header.Get(protocol.HeaderProfileExpect),
		Execution: r.Header.Get(protocol.HeaderExecution),
		Renew:     r.Header.Get(protocol.HeaderRenew),
		Features:  protocol.ParseFeatures(r.Header.Get(protocol.HeaderClientFeatures)),
		RequestID: rid,
		Client:    r.Header.Get(protocol.HeaderClient),
	})
	if err != nil {
		if problem.From(err).Code == protocol.CodeUnauthenticated {
			w.Header().Set("WWW-Authenticate", `Bearer realm="atlas"`)
		}
		problem.Write(w, rid, err)
		return
	}
	if out.Writer != "" {
		w.Header().Set(protocol.HeaderProfile, out.Writer)
	}
	switch a := out.Answer; {
	case a.Handoff != nil:
		// A credential for one result: never cached (§2.2.1, rule 3).
		w.Header().Set("Cache-Control", "no-store")
		out.Finish(0, nil)
		writeJSON(w, http.StatusOK, protocol.MediaHandoff, a.Handoff)
	case a.Table != nil:
		defer a.Table.Release()
		if strings.Contains(r.Header.Get("Accept"), protocol.MediaArrowStream) {
			e.streamArrow(w, out)
		} else {
			e.streamNDJSON(w, out)
		}
	default:
		out.Finish(0, nil)
		writeJSON(w, http.StatusOK, protocol.MediaJSON, a.Record)
	}
}

// streamNDJSON passes each batch on as it arrives, never buffering the whole
// answer (ATLAS-GATEWAY-005), and ends with the end line. A failure after the
// first row travels in the end line, because the status is already sent.
func (e *Edge) streamNDJSON(w http.ResponseWriter, out *pipeline.Outcome) {
	t := out.Answer.Table
	w.Header().Set("Content-Type", protocol.MediaNDJSON)
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	var rows int64
	var buf bytes.Buffer
	var ferr error
	for ferr == nil && t.Next() {
		rec := t.RecordBatch()
		buf.Reset()
		for i := range int(rec.NumRows()) {
			if ferr = protocol.AppendRowJSON(&buf, rec, i); ferr != nil {
				break
			}
			buf.WriteByte('\n')
		}
		if ferr == nil {
			if _, ferr = w.Write(buf.Bytes()); ferr == nil {
				rows += rec.NumRows()
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	if ferr == nil {
		ferr = t.Err()
	}
	end := protocol.EndLine{Atlas: "end", Rows: rows}
	if ferr != nil {
		end.Problem = problem.From(ferr)
	}
	out.Finish(rows, ferr)
	_ = json.NewEncoder(w).Encode(end)
}

// streamArrow writes an Arrow IPC stream with ZSTD buffers, ending with the
// final zero-row batch that carries the end line (§9.6.5). A failure after
// the first batch travels in it, because the status is already sent.
func (e *Edge) streamArrow(w http.ResponseWriter, out *pipeline.Outcome) {
	t := out.Answer.Table
	w.Header().Set("Content-Type", protocol.MediaArrowStream)
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	iw := ipc.NewWriter(w, ipc.WithSchema(t.Schema()), ipc.WithAllocator(e.Mem), ipc.WithZstd())
	defer iw.Close()
	var rows int64
	var ferr error
	for ferr == nil && t.Next() {
		rec := t.RecordBatch()
		if ferr = iw.Write(rec); ferr == nil {
			rows += rec.NumRows()
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	if ferr == nil {
		ferr = t.Err()
	}
	end := protocol.EndLine{Atlas: "end", Rows: rows}
	if ferr != nil {
		end.Problem = problem.From(ferr)
	}
	out.Finish(rows, ferr)
	final, err := protocol.EndBatch(e.Mem, t.Schema(), end)
	if err != nil {
		return // without its final batch the client refuses the answer
	}
	defer final.Release()
	_ = iw.Write(final)
}

func bearer(r *http.Request) string {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

func writeJSON(w http.ResponseWriter, status int, media string, v any) {
	w.Header().Set("Content-Type", media)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
