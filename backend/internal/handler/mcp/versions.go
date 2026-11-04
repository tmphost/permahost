// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The protocol version headers and _meta keys of the 2026-07-28 revision.
const (
	_headerVersion    = "Mcp-Protocol-Version"
	_headerMethod     = "Mcp-Method"
	_metaVersion      = "io.modelcontextprotocol/protocolVersion"
	_metaCapabilities = "io.modelcontextprotocol/clientCapabilities"
	// _latestVersion is the stateless revision, the default for a request
	// that declares no version.
	_latestVersion  = "2026-07-28"
	_methodDiscover = "server/discover"
)

// _newerResultFields are the result envelope fields 2026-07-28 added; an
// older client never receives them.
var _newerResultFields = []string{"resultType", "ttlMs", "cacheScope"}

// rpcRequest is the part of a JSON-RPC request that version negotiation
// reads.
type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Meta map[string]any `json:"_meta"`
	} `json:"params"`
}

// negotiate fills the gaps the SDK leaves in 2026-07-28 version
// negotiation around next:
//   - a served request's version is echoed in the Mcp-Protocol-Version
//     response header;
//   - an unsupported version gets an UnsupportedProtocolVersion error that
//     lists the versions server/discover advertises, not a plain-text 400;
//   - a server/discover that declares no version is served on 2026-07-28;
//   - a client on an older version gets no 2026-07-28 result fields.
func negotiate(next http.Handler, maxBodyBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		body := r.Body
		if maxBodyBytes > 0 {
			body = http.MaxBytesReader(w, body, maxBodyBytes)
		}
		raw, err := io.ReadAll(body)
		if err != nil {
			status := http.StatusBadRequest
			if errors.As(err, new(*http.MaxBytesError)) {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, err.Error(), status)
			return
		}
		var rq rpcRequest
		if json.Unmarshal(raw, &rq) != nil {
			// A batch or malformed body: the SDK answers it.
			r.Body = io.NopCloser(bytes.NewReader(raw))
			next.ServeHTTP(w, r)
			return
		}

		version := r.Header.Get(_headerVersion)
		metaVersion, _ := rq.Params.Meta[_metaVersion].(string)
		if version == "" {
			version = metaVersion
		}
		if version == "" && rq.Method == _methodDiscover {
			version = _latestVersion
			raw = withVersion(raw)
			r.Header.Set(_headerVersion, version)
			r.Header.Set(_headerMethod, rq.Method)
		}
		if version != "" && !slices.Contains(mcp.SupportedProtocolVersions(), version) {
			unsupported(w, rq.ID, version)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		r.ContentLength = int64(len(raw))
		if version != "" {
			w.Header().Set(_headerVersion, version)
		}
		if version >= _latestVersion {
			next.ServeHTTP(w, r)
			return
		}
		rec := &recorder{header: w.Header(), status: http.StatusOK}
		next.ServeHTTP(rec, r)
		out := withoutNewerFields(rec.body.Bytes())
		w.Header().Set("Content-Length", strconv.Itoa(len(out)))
		w.WriteHeader(rec.status)
		_, _ = w.Write(out)
	})
}

// withVersion declares 2026-07-28 in a request's _meta, as the SDK needs
// it to serve server/discover without a handshake. The body already
// parsed as a request, so it is an object.
func withVersion(raw []byte) []byte {
	var rq map[string]any
	_ = json.Unmarshal(raw, &rq)
	params, _ := rq["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	meta, _ := params["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta[_metaVersion] = _latestVersion
	if _, ok := meta[_metaCapabilities]; !ok {
		meta[_metaCapabilities] = map[string]any{}
	}
	params["_meta"] = meta
	rq["params"] = params
	out, _ := json.Marshal(rq)
	return out
}

// unsupported answers a request for a version the server does not speak
// with the versions it does, as server/discover lists them.
func unsupported(w http.ResponseWriter, id json.RawMessage, version string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	data, _ := json.Marshal(mcp.UnsupportedProtocolVersionData{Supported: mcp.SupportedProtocolVersions(), Requested: version})
	rs, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    mcp.CodeUnsupportedProtocolVersion,
			"message": fmt.Sprintf("protocol version %q is not supported", version),
			"data":    json.RawMessage(data),
		},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(rs)
}

// withoutNewerFields drops the 2026-07-28 envelope fields from a JSON-RPC
// response's result; any other body is returned as it is.
func withoutNewerFields(body []byte) []byte {
	var rs map[string]json.RawMessage
	if json.Unmarshal(body, &rs) != nil {
		return body
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(rs["result"], &result) != nil {
		return body
	}
	for _, f := range _newerResultFields {
		delete(result, f)
	}
	rs["result"], _ = json.Marshal(result)
	out, _ := json.Marshal(rs)
	return out
}

// recorder holds a response so its body can be rewritten.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) WriteHeader(status int)      { r.status = status }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }
