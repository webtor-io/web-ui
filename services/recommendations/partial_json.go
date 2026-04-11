package recommendations

import "encoding/json"

// ndjsonItemsExtractor pulls top-level JSON objects out of a streaming
// text response that's formatted as one object per line (or with arbitrary
// whitespace between objects). Every depth-0 → 1 → 0 brace cycle is a
// complete object — there is no enclosing array.
//
// This is the parser for the NDJSON-style plain-text streaming flow we
// use as a workaround for Anthropic's tool_use delta buffering: by asking
// Claude for plain text output we get true token-by-token streaming, and
// this scanner emits each film as soon as its closing brace lands.
type ndjsonItemsExtractor struct {
	buf       []byte
	pos       int
	depth     int
	inString  bool
	escape    bool
	itemStart int
	onItem    func(json.RawMessage)
}

func newNDJSONItemsExtractor(onItem func(json.RawMessage)) *ndjsonItemsExtractor {
	return &ndjsonItemsExtractor{itemStart: -1, onItem: onItem}
}

func (e *ndjsonItemsExtractor) write(chunk string) {
	if len(chunk) == 0 {
		return
	}
	e.buf = append(e.buf, chunk...)
	for ; e.pos < len(e.buf); e.pos++ {
		b := e.buf[e.pos]
		if e.inString {
			if e.escape {
				e.escape = false
				continue
			}
			if b == '\\' {
				e.escape = true
				continue
			}
			if b == '"' {
				e.inString = false
			}
			continue
		}
		switch b {
		case '"':
			e.inString = true
		case '{':
			if e.depth == 0 {
				e.itemStart = e.pos
			}
			e.depth++
		case '}':
			e.depth--
			if e.depth == 0 && e.itemStart >= 0 {
				end := e.pos + 1
				obj := make([]byte, end-e.itemStart)
				copy(obj, e.buf[e.itemStart:end])
				e.itemStart = -1
				if e.onItem != nil {
					e.onItem(obj)
				}
			}
		}
	}
}

