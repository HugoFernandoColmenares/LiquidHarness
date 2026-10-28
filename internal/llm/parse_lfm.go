// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

const (
	// ToolCallStart opens the LFM2.5 tool-call block.
	ToolCallStart = "<|tool_call_start|>"
	// ToolCallEnd closes the LFM2.5 tool-call block.
	ToolCallEnd = "<|tool_call_end|>"
	// ThinkStart opens a reasoning block.
	ThinkStart = " thinking"
	// ThinkEnd closes a reasoning block.
	ThinkEnd = " response"
	// MaxToolCalls is the hard limit per model response.
	MaxToolCalls = 8
)

var toolNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// Parsed is the canonical interpretation of a raw model response: visible text, reasoning and
// validated tool calls. Content never contains the special tokens.
type Parsed struct {
	Content   string
	Reasoning string
	ToolCalls []core.ToolCall
}

// ParseError is returned when a response contains a tool-call block that cannot be interpreted.
// The raw block is preserved for audit and for a precise user-facing message.
type ParseError struct {
	Raw string
	Err error
}

// Error implements error.
func (e *ParseError) Error() string {
	return fmt.Sprintf("invalid tool call %q: %v", snippet(e.Raw), e.Err)
}

// Unwrap returns the underlying cause.
func (e *ParseError) Unwrap() error { return e.Err }

// Parse extracts reasoning, visible content and tool calls from a raw LFM2.5 response. It accepts
// both documented formats (JSON and Pythonic) and never returns a partial list: if one call is
// malformed, the whole response is rejected.
func Parse(raw string) (Parsed, error) {
	text, reasoning, err := extractThinking(raw)
	if err != nil {
		return Parsed{}, &ParseError{Raw: raw, Err: err}
	}

	text, blocks, err := extractToolBlocks(text)
	if err != nil {
		return Parsed{}, &ParseError{Raw: raw, Err: err}
	}

	calls := make([]core.ToolCall, 0, len(blocks))
	for _, block := range blocks {
		parsed, parseErr := parseBlock(block)
		if parseErr != nil {
			return Parsed{}, &ParseError{Raw: block, Err: parseErr}
		}
		calls = append(calls, parsed...)
	}

	if len(blocks) == 0 {
		if block, start, end, ok := findFencedJSON(text); ok {
			parsed, parseErr := parseBlock(block)
			if parseErr != nil {
				return Parsed{}, &ParseError{Raw: block, Err: parseErr}
			}
			calls = append(calls, parsed...)
			text = text[:start] + text[end:]
		}
	}

	if len(calls) > MaxToolCalls {
		return Parsed{}, &ParseError{
			Raw: raw,
			Err: fmt.Errorf("model returned %d tool calls; the limit is %d", len(calls), MaxToolCalls),
		}
	}

	return Parsed{
		Content:   strings.TrimSpace(text),
		Reasoning: strings.TrimSpace(reasoning),
		ToolCalls: calls,
	}, nil
}

func extractThinking(text string) (string, string, error) {
	var reasoning []string
	var out strings.Builder

	for {
		start := strings.Index(text, ThinkStart)
		if start < 0 {
			out.WriteString(text)
			break
		}
		rest := text[start+len(ThinkStart):]
		end := strings.Index(rest, ThinkEnd)
		if end < 0 {
			return "", "", errors.New("unterminated thinking block")
		}
		out.WriteString(text[:start])
		reasoning = append(reasoning, rest[:end])
		text = rest[end+len(ThinkEnd):]
	}

	return out.String(), strings.Join(reasoning, "\n"), nil
}

func extractToolBlocks(text string) (string, []string, error) {
	var blocks []string
	var out strings.Builder

	for {
		start := strings.Index(text, ToolCallStart)
		if start < 0 {
			out.WriteString(text)
			break
		}
		out.WriteString(text[:start])
		rest := text[start+len(ToolCallStart):]
		end := strings.Index(rest, ToolCallEnd)
		if end < 0 {
			return "", nil, errors.New("unterminated tool call block")
		}
		blocks = append(blocks, rest[:end])
		out.WriteString(" ")
		text = rest[end+len(ToolCallEnd):]
	}

	return out.String(), blocks, nil
}

func findFencedJSON(text string) (block string, start, end int, ok bool) {
	const fence = "```json"
	index := strings.Index(text, fence)
	if index < 0 {
		return "", 0, 0, false
	}
	closeIndex := strings.Index(text[index+len(fence):], "```")
	if closeIndex < 0 {
		return "", 0, 0, false
	}
	bodyStart := index + len(fence)
	bodyEnd := bodyStart + closeIndex
	body := text[bodyStart:bodyEnd]
	if !strings.Contains(body, `"name"`) {
		return "", 0, 0, false
	}
	return body, index, bodyEnd + len("```"), true
}

func parseBlock(block string) ([]core.ToolCall, error) {
	trimmed := strings.TrimSpace(block)
	if trimmed == "" {
		return nil, errors.New("empty tool call block")
	}

	var jsonErr error
	if trimmed[0] == '[' || trimmed[0] == '{' {
		calls, err := parseJSONCalls(trimmed, block)
		if err == nil {
			return calls, nil
		}
		jsonErr = err
	}

	pythonic, pyErr := parsePythonicCalls(trimmed, block)
	if pyErr == nil {
		return pythonic, nil
	}
	if jsonErr != nil {
		return nil, fmt.Errorf("invalid tool call block: %w", errors.Join(
			fmt.Errorf("json: %w", jsonErr),
			fmt.Errorf("pythonic: %w", pyErr),
		))
	}
	return nil, pyErr
}

func parseJSONCalls(block, raw string) ([]core.ToolCall, error) {
	decoder := json.NewDecoder(strings.NewReader(block))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, err
	}

	var items []any
	switch typed := value.(type) {
	case []any:
		items = typed
	case map[string]any:
		if nested, ok := typed["tool_calls"].([]any); ok {
			items = nested
		} else {
			items = []any{typed}
		}
	default:
		return nil, fmt.Errorf("expected a JSON array or object, got %T", value)
	}

	calls := make([]core.ToolCall, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool call %d is not a JSON object", index)
		}
		call, err := callFromJSONObject(object, raw)
		if err != nil {
			return nil, fmt.Errorf("tool call %d: %w", index, err)
		}
		calls = append(calls, call)
	}
	if len(calls) > MaxToolCalls {
		return nil, fmt.Errorf("model returned %d tool calls; the limit is %d", len(calls), MaxToolCalls)
	}
	return calls, nil
}

func callFromJSONObject(object map[string]any, raw string) (core.ToolCall, error) {
	name, _ := object["name"].(string)
	arguments := object["arguments"]

	if function, ok := object["function"].(map[string]any); ok {
		if name == "" {
			name, _ = function["name"].(string)
		}
		if arguments == nil {
			arguments = function["arguments"]
		}
	}
	if name == "" {
		return core.ToolCall{}, errors.New("missing name")
	}
	if !toolNamePattern.MatchString(name) {
		return core.ToolCall{}, fmt.Errorf("invalid tool name %q", name)
	}

	normalized, err := argumentsFromAny(arguments)
	if err != nil {
		return core.ToolCall{}, err
	}

	id, _ := object["id"].(string)
	if id == "" {
		id = core.NewID()
	}

	return core.ToolCall{
		ID:        id,
		Name:      name,
		Arguments: normalized,
		Raw:       raw,
	}, nil
}

func argumentsFromAny(value any) (json.RawMessage, error) {
	switch typed := value.(type) {
	case nil:
		return json.RawMessage("{}"), nil
	case string:
		return normalizeArguments(typed)
	case map[string]any, []any:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		return normalizeArguments(string(encoded))
	default:
		return nil, fmt.Errorf("arguments must be an object, got %T", value)
	}
}

func normalizeArguments(raw string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("{}"), nil
	}

	// Some gateways serialize the object twice. Unwrap at most two string layers so a
	// pathological input cannot loop.
	for range 2 {
		if !strings.HasPrefix(trimmed, `"`) {
			break
		}
		var text string
		if err := json.Unmarshal([]byte(trimmed), &text); err != nil {
			return nil, err
		}
		trimmed = strings.TrimSpace(text)
	}

	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, err
	}

	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("arguments must be a JSON object, got %T", value)
	}
	if object == nil {
		object = map[string]any{}
	}

	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON")
	}
	return err
}

func parsePythonicCalls(block, raw string) ([]core.ToolCall, error) {
	trimmed := strings.TrimSpace(block)
	if len(trimmed) < 2 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
		return nil, errors.New("expected a Python-style list of calls")
	}

	inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	if inner == "" {
		return nil, nil
	}

	items, err := splitTopLevel(inner, ',')
	if err != nil {
		return nil, err
	}

	calls := make([]core.ToolCall, 0, len(items))
	for index, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		call, err := parsePythonicCall(item, raw)
		if err != nil {
			return nil, fmt.Errorf("call %d: %w", index, err)
		}
		calls = append(calls, call)
	}
	if len(calls) > MaxToolCalls {
		return nil, fmt.Errorf("model returned %d tool calls; the limit is %d", len(calls), MaxToolCalls)
	}
	return calls, nil
}

func parsePythonicCall(item, raw string) (core.ToolCall, error) {
	open := strings.Index(item, "(")
	if open < 0 {
		return core.ToolCall{}, errors.New("missing opening parenthesis")
	}
	if !strings.HasSuffix(item, ")") {
		return core.ToolCall{}, errors.New("missing closing parenthesis")
	}

	name := strings.TrimSpace(item[:open])
	if !toolNamePattern.MatchString(name) {
		return core.ToolCall{}, fmt.Errorf("invalid tool name %q", name)
	}

	args, err := parseKeywordArgs(item[open+1 : len(item)-1])
	if err != nil {
		return core.ToolCall{}, err
	}

	encoded, err := json.Marshal(args)
	if err != nil {
		return core.ToolCall{}, err
	}

	return core.ToolCall{
		ID:        core.NewID(),
		Name:      name,
		Arguments: encoded,
		Raw:       raw,
	}, nil
}

func parseKeywordArgs(input string) (map[string]any, error) {
	parser := &pythonParser{input: input}
	args := map[string]any{}

	for {
		parser.skipSpace()
		if parser.done() {
			return args, nil
		}

		key := parser.identifier()
		if key == "" {
			return nil, errors.New("expected a keyword argument")
		}
		parser.skipSpace()
		if !parser.consume('=') {
			return nil, fmt.Errorf("expected '=' after %q", key)
		}

		value, err := parser.value()
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", key, err)
		}
		args[key] = value

		parser.skipSpace()
		if parser.done() {
			return args, nil
		}
		if !parser.consume(',') {
			return nil, errors.New("expected ',' between arguments")
		}
	}
}

type pythonParser struct {
	input string
	pos   int
}

func (p *pythonParser) done() bool { return p.pos >= len(p.input) }

func (p *pythonParser) skipSpace() {
	for !p.done() && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}

func (p *pythonParser) consume(want byte) bool {
	if p.done() || p.input[p.pos] != want {
		return false
	}
	p.pos++
	return true
}

func (p *pythonParser) identifier() string {
	start := p.pos
	if p.done() || !isIdentifierStart(p.input[p.pos]) {
		return ""
	}
	p.pos++
	for !p.done() && isIdentifierPart(p.input[p.pos]) {
		p.pos++
	}
	return p.input[start:p.pos]
}

func (p *pythonParser) value() (any, error) {
	p.skipSpace()
	if p.done() {
		return nil, errors.New("missing value")
	}

	switch p.input[p.pos] {
	case '\'', '"':
		return p.stringLiteral()
	case '[':
		return p.listLiteral()
	case '(':
		return p.tupleLiteral()
	case '{':
		return p.dictLiteral()
	default:
		return p.scalar()
	}
}

func (p *pythonParser) stringLiteral() (string, error) {
	quote := p.input[p.pos]
	p.pos++

	var out strings.Builder
	for !p.done() {
		char := p.input[p.pos]
		p.pos++
		switch char {
		case quote:
			return out.String(), nil
		case '\\':
			escaped, err := p.escape()
			if err != nil {
				return "", err
			}
			out.WriteString(escaped)
		default:
			out.WriteByte(char)
		}
	}
	return "", errors.New("unterminated string")
}

func (p *pythonParser) escape() (string, error) {
	if p.done() {
		return "", errors.New("unterminated escape")
	}
	char := p.input[p.pos]
	p.pos++

	switch char {
	case '\\', '\'', '"', '/':
		return string(char), nil
	case 'n':
		return "\n", nil
	case 'r':
		return "\r", nil
	case 't':
		return "\t", nil
	case 'b':
		return "\b", nil
	case 'f':
		return "\f", nil
	case 'u':
		return p.unicodeEscape(4)
	case 'x':
		return p.unicodeEscape(2)
	default:
		return "", fmt.Errorf("unsupported escape \\%c", char)
	}
}

func (p *pythonParser) unicodeEscape(digits int) (string, error) {
	if p.pos+digits > len(p.input) {
		return "", errors.New("truncated unicode escape")
	}
	value, err := strconv.ParseUint(p.input[p.pos:p.pos+digits], 16, 32)
	if err != nil {
		return "", fmt.Errorf("invalid unicode escape: %w", err)
	}
	p.pos += digits
	return string(rune(value)), nil //nolint:gosec // value is at most four hexadecimal digits.
}

func (p *pythonParser) listLiteral() ([]any, error) {
	return p.sequence('[', ']')
}

func (p *pythonParser) tupleLiteral() ([]any, error) {
	return p.sequence('(', ')')
}

func (p *pythonParser) sequence(open, close byte) ([]any, error) {
	if !p.consume(open) {
		return nil, fmt.Errorf("expected %q", open)
	}

	items := []any{}
	for {
		p.skipSpace()
		if p.consume(close) {
			return items, nil
		}
		value, err := p.value()
		if err != nil {
			return nil, err
		}
		items = append(items, value)

		p.skipSpace()
		if p.consume(close) {
			return items, nil
		}
		if !p.consume(',') {
			return nil, fmt.Errorf("expected ',' or %q", close)
		}
	}
}

func (p *pythonParser) dictLiteral() (map[string]any, error) {
	if !p.consume('{') {
		return nil, errors.New("expected '{'")
	}

	object := map[string]any{}
	for {
		p.skipSpace()
		if p.consume('}') {
			return object, nil
		}

		key, err := p.dictKey()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if !p.consume(':') {
			return nil, errors.New("expected ':' in dictionary")
		}
		value, err := p.value()
		if err != nil {
			return nil, err
		}
		object[key] = value

		p.skipSpace()
		if p.consume('}') {
			return object, nil
		}
		if !p.consume(',') {
			return nil, errors.New("expected ',' or '}'")
		}
	}
}

func (p *pythonParser) dictKey() (string, error) {
	p.skipSpace()
	if p.done() {
		return "", errors.New("missing dictionary key")
	}
	if p.input[p.pos] == '\'' || p.input[p.pos] == '"' {
		return p.stringLiteral()
	}
	key := p.identifier()
	if key == "" {
		return "", errors.New("dictionary key must be a string or identifier")
	}
	return key, nil
}

func (p *pythonParser) scalar() (any, error) {
	start := p.pos
	for !p.done() {
		char := p.input[p.pos]
		if char == ',' || char == ']' || char == ')' || char == '}' || unicode.IsSpace(rune(char)) {
			break
		}
		p.pos++
	}

	token := p.input[start:p.pos]
	switch strings.ToLower(token) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "none", "null":
		return nil, nil
	}

	if number, err := strconv.ParseInt(token, 10, 64); err == nil {
		return number, nil
	}
	if number, err := strconv.ParseFloat(token, 64); err == nil {
		return number, nil
	}
	return nil, fmt.Errorf("unsupported literal %q", token)
}

func splitTopLevel(input string, separator byte) ([]string, error) {
	var parts []string
	start := 0
	depth := 0
	var quote byte
	escaped := false

	for index := 0; index < len(input); index++ {
		char := input[index]
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case char == '\\':
				escaped = true
			case char == quote:
				quote = 0
			}
			continue
		}

		switch char {
		case '\'', '"':
			quote = char
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced %q", char)
			}
		default:
			if char == separator && depth == 0 {
				parts = append(parts, input[start:index])
				start = index + 1
			}
		}
	}

	if quote != 0 {
		return nil, errors.New("unterminated string")
	}
	if depth != 0 {
		return nil, errors.New("unbalanced brackets")
	}
	return append(parts, input[start:]), nil
}

func isIdentifierStart(char byte) bool {
	return char == '_' || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func isIdentifierPart(char byte) bool {
	return isIdentifierStart(char) || (char >= '0' && char <= '9')
}

func snippet(value string) string {
	trimmed := strings.TrimSpace(value)
	runes := []rune(trimmed)
	if len(runes) <= 160 {
		return trimmed
	}
	return string(runes[:160]) + "..."
}

// markerFilter removes tool-call blocks and thinking blocks from a streamed content channel. It
// holds back a suffix that could be the beginning of a marker, so a marker split across two
// deltas is never printed to the user.
type markerFilter struct {
	buf   string
	state filterState
}

type filterState int

const (
	filterText filterState = iota
	filterTool
	filterThink
)

type filterMarker struct {
	start string
	state filterState
}

var filterMarkers = []filterMarker{
	{start: ToolCallStart, state: filterTool},
	{start: ThinkStart, state: filterThink},
}

// Write accepts a content delta and returns the visible text that is safe to print.
func (f *markerFilter) Write(chunk string) string {
	f.buf += chunk
	var out strings.Builder

	for {
		switch f.state {
		case filterText:
			index, marker := earliestMarker(f.buf)
			if index < 0 {
				keep := partialMarkerSuffix(f.buf)
				if len(f.buf) > keep {
					out.WriteString(f.buf[:len(f.buf)-keep])
				}
				f.buf = f.buf[len(f.buf)-keep:]
				return out.String()
			}
			out.WriteString(f.buf[:index])
			f.buf = f.buf[index+len(marker.start):]
			f.state = marker.state
		case filterTool:
			index := strings.Index(f.buf, ToolCallEnd)
			if index < 0 {
				return out.String()
			}
			f.buf = f.buf[index+len(ToolCallEnd):]
			f.state = filterText
		case filterThink:
			index := strings.Index(f.buf, ThinkEnd)
			if index < 0 {
				return out.String()
			}
			f.buf = f.buf[index+len(ThinkEnd):]
			f.state = filterText
		}
	}
}

// Flush returns any held-back text. It fails when the stream ended inside a marker: that means a
// truncated tool call, which must never be executed.
func (f *markerFilter) Flush() (string, error) {
	switch f.state {
	case filterText:
		rest := f.buf
		f.buf = ""
		return rest, nil
	case filterTool:
		return "", errors.New("stream ended inside a tool call block")
	case filterThink:
		return "", errors.New("stream ended inside a thinking block")
	default:
		return "", nil
	}
}

func earliestMarker(input string) (int, filterMarker) {
	best := -1
	var selected filterMarker
	for _, marker := range filterMarkers {
		index := strings.Index(input, marker.start)
		if index < 0 {
			continue
		}
		if best < 0 || index < best {
			best = index
			selected = marker
		}
	}
	return best, selected
}

func partialMarkerSuffix(input string) int {
	best := 0
	for _, marker := range filterMarkers {
		limit := len(marker.start)
		if len(input) < limit {
			limit = len(input)
		}
		for length := limit; length > best; length-- {
			if strings.HasSuffix(input, marker.start[:length]) {
				best = length
				break
			}
		}
	}
	return best
}
