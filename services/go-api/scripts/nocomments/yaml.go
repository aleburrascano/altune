package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

const yamlUTF8BOM = "\xEF\xBB\xBF"

func init() {
	register(workflowKind)
	register(yamlKind)
}

var workflowKind = kind{
	name:      "workflow",
	match:     matchWorkflowFile,
	comments:  workflowComments,
	same:      workflowSame,
	leftovers: workflowLeftovers,
}

var yamlKind = kind{
	name:     "yaml",
	match:    matchPlainYamlFile,
	comments: yamlComments,
	same:     yamlSame,
}

var workflowDirs = []string{".github/workflows/", ".github/actions/", ".gitea/workflows/", ".gitea/actions/"}

func isYamlPath(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yml" || ext == ".yaml"
}

func matchWorkflowFile(path string, _ []byte) bool {
	if !isYamlPath(path) {
		return false
	}
	slashed := filepath.ToSlash(path)
	for _, dir := range workflowDirs {
		if strings.Contains(slashed, dir) {
			return true
		}
	}
	return false
}

func matchPlainYamlFile(path string, head []byte) bool {
	return isYamlPath(path) && !matchWorkflowFile(path, head)
}

type yamlBlock struct{ header, first, last, indent int }

type yamlLine struct{ start, end, number int }

type yamlScanner struct {
	src         []byte
	spans       []span
	blocks      []yamlBlock
	err         error
	inSingle    bool
	inDouble    bool
	flow        int
	scalarStart bool
	plainOpen   bool
	plainParent int
	sawKey      bool
	entryCol    int
	parent      int
	open        *yamlBlock
	openParent  int
}

func yamlComments(src []byte) ([]span, error) {
	spans, _, err := yamlScan(src)
	return spans, err
}

func yamlScan(src []byte) ([]span, []yamlBlock, error) {
	if err := rejectLoneCR(src); err != nil {
		return nil, nil, err
	}
	sc := &yamlScanner{src: src}
	number := 0
	start := 0
	if bytes.HasPrefix(src, []byte(yamlUTF8BOM)) {
		start = len(yamlUTF8BOM)
	}
	for start < len(src) && sc.err == nil {
		number++
		end := lineEndOffset(src, start)
		sc.scanLine(yamlLine{start, contentEnd(src, start, end), number})
		start = end + 1
	}
	if sc.err == nil && (sc.inSingle || sc.inDouble) {
		sc.err = errors.New("unterminated quoted scalar at end of file")
	}
	if sc.err != nil {
		return nil, nil, sc.err
	}
	sc.closeBlock()
	return sc.spans, sc.blocks, nil
}

func rejectLoneCR(src []byte) error {
	for i, line := 0, 1; i < len(src); i++ {
		switch {
		case src[i] == '\n':
			line++
		case src[i] == '\r' && (i+1 >= len(src) || src[i+1] != '\n'):
			return fmt.Errorf("lone CR line ending at line %d", line)
		}
	}
	return nil
}

func contentEnd(src []byte, start, end int) int {
	if end > start && src[end-1] == '\r' {
		return end - 1
	}
	return end
}

func isBlankByte(c byte) bool { return c == ' ' || c == '\t' }

func skipBlanks(src []byte, i, end int) int {
	for i < end && isBlankByte(src[i]) {
		i++
	}
	return i
}

func indicatorEnd(src []byte, i, end int) bool {
	return i+1 >= end || isBlankByte(src[i+1])
}

func (sc *yamlScanner) closeBlock() {
	if sc.open != nil {
		sc.blocks = append(sc.blocks, *sc.open)
		sc.open = nil
	}
}

func (sc *yamlScanner) scanLine(ln yamlLine) {
	if sc.open != nil && sc.consumeBody(ln) {
		return
	}
	i := ln.start
	if !sc.inSingle && !sc.inDouble {
		i = sc.scanLineHead(ln)
	}
	sc.scanRest(ln, i)
}

func (sc *yamlScanner) consumeBody(ln yamlLine) bool {
	text := sc.src[ln.start:ln.end]
	if isBlank(text) {
		return true
	}
	indent := skipBlanks(sc.src, ln.start, ln.end) - ln.start
	b := sc.open
	if b.first == 0 {
		if indent <= sc.openParent {
			sc.closeBlock()
			return false
		}
		b.first, b.indent = ln.number, indent
	} else if indent < b.indent {
		sc.closeBlock()
		return false
	}
	b.last = ln.number
	return true
}

func (sc *yamlScanner) scanLineHead(ln yamlLine) int {
	i := skipBlanks(sc.src, ln.start, ln.end)
	if sc.plainOpen && sc.flow == 0 {
		if i >= ln.end {
			return i
		}
		if i-ln.start > sc.plainParent {
			sc.scalarStart = false
			return i
		}
		sc.plainOpen = false
	}
	sc.parent = -1
	sc.sawKey = false
	if sc.flow == 0 {
		sc.scalarStart = true
		if isDocumentMarker(sc.src, ln, i) {
			return skipBlanks(sc.src, i+3, ln.end)
		}
		for i < ln.end && (sc.src[i] == '-' || sc.src[i] == '?') && indicatorEnd(sc.src, i, ln.end) {
			sc.parent = i - ln.start
			i = skipBlanks(sc.src, i+1, ln.end)
		}
		sc.entryCol = i - ln.start
	}
	return i
}

func isDocumentMarker(src []byte, ln yamlLine, i int) bool {
	if i != ln.start || ln.end-i < 3 {
		return false
	}
	marker := string(src[i : i+3])
	return (marker == "---" || marker == "...") && indicatorEnd(src, i+2, ln.end)
}

func (sc *yamlScanner) scanRest(ln yamlLine, i int) {
	for i < ln.end && sc.err == nil {
		switch {
		case sc.inSingle:
			i = sc.stepSingle(i, ln.end)
		case sc.inDouble:
			i = sc.stepDouble(i)
		default:
			next, stop := sc.stepPlain(ln, i)
			if stop {
				return
			}
			i = next
		}
	}
}

func (sc *yamlScanner) stepSingle(i, end int) int {
	if sc.src[i] != '\'' {
		return i + 1
	}
	if i+1 < end && sc.src[i+1] == '\'' {
		return i + 2
	}
	sc.inSingle = false
	sc.scalarStart = false
	return i + 1
}

func (sc *yamlScanner) stepDouble(i int) int {
	switch sc.src[i] {
	case '\\':
		return i + 2
	case '"':
		sc.inDouble = false
		sc.scalarStart = false
	}
	return i + 1
}

func (sc *yamlScanner) stepPlain(ln yamlLine, i int) (int, bool) {
	c := sc.src[i]
	switch {
	case isBlankByte(c):
		return i + 1, false
	case c == '#' && (i == ln.start || isBlankByte(sc.src[i-1])):
		s, _ := tomlCommentSpan(sc.src, ln.start, i, ln.number)
		sc.spans = append(sc.spans, s)
		return 0, true
	case sc.scalarStart:
		return sc.stepScalarStart(ln, i)
	}
	return sc.stepText(ln, i), false
}

func (sc *yamlScanner) stepScalarStart(ln yamlLine, i int) (int, bool) {
	switch sc.src[i] {
	case '\'':
		sc.inSingle = true
		return i + 1, false
	case '"':
		sc.inDouble = true
		return i + 1, false
	case '|', '>':
		if sc.flow == 0 {
			return sc.blockHeader(ln, i)
		}
	case '&', '!':
		return sc.skipWord(ln, i), false
	case '[', '{':
		sc.flow++
		return i + 1, false
	}
	sc.scalarStart = false
	return sc.stepText(ln, i), false
}

func (sc *yamlScanner) skipWord(ln yamlLine, i int) int {
	for i < ln.end && !isBlankByte(sc.src[i]) {
		i++
	}
	return i
}

func (sc *yamlScanner) stepText(ln yamlLine, i int) int {
	if !sc.plainOpen && sc.flow == 0 {
		sc.plainOpen, sc.plainParent = true, sc.parent
	}
	sc.scalarStart = false
	switch sc.src[i] {
	case ':':
		if sc.flow > 0 || indicatorEnd(sc.src, i, ln.end) {
			sc.markKey()
		}
	case ',':
		sc.scalarStart = sc.flow > 0
	case ']', '}':
		if sc.flow > 0 {
			sc.flow--
		}
	}
	return i + 1
}

func (sc *yamlScanner) markKey() {
	sc.scalarStart = true
	sc.plainOpen = false
	if !sc.sawKey && sc.flow == 0 {
		sc.parent = sc.entryCol
	}
	sc.sawKey = true
}

func (sc *yamlScanner) blockHeader(ln yamlLine, i int) (int, bool) {
	j := i + 1
	for j < ln.end && (sc.src[j] == '+' || sc.src[j] == '-') {
		j++
	}
	switch {
	case j < ln.end && sc.src[j] >= '1' && sc.src[j] <= '9':
		sc.err = fmt.Errorf("line %d: explicit block indentation indicator unsupported", ln.number)
		return 0, true
	case j < ln.end && !isBlankByte(sc.src[j]):
		sc.err = fmt.Errorf("line %d: malformed block scalar header", ln.number)
		return 0, true
	}
	sc.scalarStart = false
	sc.open = &yamlBlock{header: ln.number}
	sc.openParent = sc.parent
	return j, false
}

type shellHit struct {
	line  int
	whole bool
}

func workflowComments(src []byte) ([]span, error) {
	spans, _, err := workflowScan(src)
	return spans, err
}

func workflowLeftovers(src []byte) ([]int, error) {
	_, lines, err := workflowScan(src)
	return lines, err
}

func workflowScan(src []byte) ([]span, []int, error) {
	spans, blocks, err := yamlScan(src)
	if err != nil {
		return nil, nil, err
	}
	runs, err := literalRuns(src)
	if err != nil {
		return nil, nil, err
	}
	starts := lineStarts(src)
	var leftovers []int
	for _, b := range blocks {
		node, ok := runs[b.header]
		if !ok {
			continue
		}
		hits, err := blockShellHits(src, starts, b, node)
		if err != nil {
			return nil, nil, err
		}
		for _, h := range hits {
			line := b.first + h.line - 1
			if h.whole {
				spans = append(spans, sourceLineSpan(src, starts, line))
			} else {
				leftovers = append(leftovers, line)
			}
		}
	}
	if err := ensureEveryRunHasBlock(runs, blocks); err != nil {
		return nil, nil, err
	}
	return spans, leftovers, nil
}

func ensureEveryRunHasBlock(runs map[int]*yaml.Node, blocks []yamlBlock) error {
	seen := map[int]bool{}
	for _, b := range blocks {
		seen[b.header] = true
	}
	for line := range runs {
		if !seen[line] {
			return fmt.Errorf("run: block at line %d not found by the scanner", line)
		}
	}
	return nil
}

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func sourceLineSpan(src []byte, starts []int, line int) span {
	start := starts[line-1]
	end := lineEndOffset(src, start)
	if end < len(src) {
		end++
	}
	return span{start, end, line}
}

func literalRuns(src []byte) (map[int]*yaml.Node, error) {
	docs, err := decodeYamlDocs(src)
	if err != nil {
		return nil, err
	}
	runs := map[int]*yaml.Node{}
	for _, doc := range docs {
		if err := collectStepRuns(doc, noStepContext, runs); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

type stepContext int

const (
	noStepContext stepContext = iota
	stepsSequence
	stepMapping
)

func collectStepRuns(n *yaml.Node, ctx stepContext, runs map[int]*yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return collectChildRuns(n, childStepContext(n, ctx), runs)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, valueNode := n.Content[i], n.Content[i+1]
		if ctx == stepMapping && key.Value == "run" {
			if err := recordRun(valueNode, runs); err != nil {
				return err
			}
			continue
		}
		next := noStepContext
		if key.Value == "steps" && valueNode.Kind == yaml.SequenceNode {
			next = stepsSequence
		}
		if err := collectStepRuns(valueNode, next, runs); err != nil {
			return err
		}
	}
	return nil
}

func childStepContext(n *yaml.Node, ctx stepContext) stepContext {
	if n.Kind == yaml.SequenceNode && ctx == stepsSequence {
		return stepMapping
	}
	return noStepContext
}

func collectChildRuns(n *yaml.Node, ctx stepContext, runs map[int]*yaml.Node) error {
	for _, child := range n.Content {
		if err := collectStepRuns(child, ctx, runs); err != nil {
			return err
		}
	}
	return nil
}

func recordRun(v *yaml.Node, runs map[int]*yaml.Node) error {
	switch {
	case v.Kind != yaml.ScalarNode:
		return nil
	case v.Style&yaml.FoldedStyle != 0:
		return fmt.Errorf("line %d: folded run: block unsupported", v.Line)
	case v.Style&yaml.LiteralStyle != 0:
		runs[v.Line] = v
	}
	return nil
}

func blockShellHits(src []byte, starts []int, b yamlBlock, node *yaml.Node) ([]shellHit, error) {
	if b.first == 0 {
		return nil, nil
	}
	text := dedentedBlock(src, starts, b)
	if strings.TrimRight(text, "\n") != strings.TrimRight(node.Value, "\n") {
		return nil, fmt.Errorf("run: block at line %d: source does not match its decoded value", b.header)
	}
	hits, err := shellCommentHits([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("run: block at line %d: %w", b.header, err)
	}
	return hits, nil
}

func dedentedBlock(src []byte, starts []int, b yamlBlock) string {
	var out strings.Builder
	for line := b.first; line <= b.last; line++ {
		start := starts[line-1]
		text := src[start:contentEnd(src, start, lineEndOffset(src, start))]
		drop := min(skipBlanks(text, 0, len(text)), b.indent)
		out.Write(text[drop:])
		out.WriteByte('\n')
	}
	return out.String()
}

func shellCommentHits(text []byte) ([]shellHit, error) {
	masked, err := maskExpressions(text)
	if err != nil {
		return nil, err
	}
	spans, err := shellComments(masked)
	if err != nil {
		return nil, err
	}
	hits := make([]shellHit, 0, len(spans))
	for _, s := range spans {
		hash := skipBlanks(masked, s.start, len(masked))
		lineStart := bytes.LastIndexByte(masked[:hash], '\n') + 1
		hits = append(hits, shellHit{line: s.line, whole: isBlank(masked[lineStart:hash])})
	}
	return hits, nil
}

func maskExpressions(text []byte) ([]byte, error) {
	out := append([]byte(nil), text...)
	for i := 0; i < len(out); {
		open := bytes.Index(out[i:], []byte("${{"))
		if open < 0 {
			break
		}
		open += i
		end, err := expressionEnd(out, open)
		if err != nil {
			return nil, err
		}
		for k := open; k < end; k++ {
			if out[k] != '\n' {
				out[k] = 'X'
			}
		}
		i = end
	}
	return out, nil
}

func expressionEnd(text []byte, open int) (int, error) {
	quoted := false
	for i := open + 3; i < len(text); i++ {
		switch {
		case text[i] == '\'':
			quoted = !quoted
		case !quoted && text[i] == '}' && i+1 < len(text) && text[i+1] == '}':
			return i + 2, nil
		}
	}
	return 0, fmt.Errorf("unterminated ${{ expression at byte %d", open)
}

func decodeYamlDocs(src []byte) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, err
		}
		docs = append(docs, &doc)
	}
}

func yamlSame(before, after []byte) error {
	return yamlTreesSame(before, after, false)
}

func workflowSame(before, after []byte) error {
	if err := blockIndentsSame(before, after); err != nil {
		return err
	}
	return yamlTreesSame(before, after, true)
}

func blockIndentKept(b, a yamlBlock) error {
	switch {
	case b.first > 0 && a.first == 0:
		return fmt.Errorf("block at line %d holds only comments; stripping would empty it", b.header)
	case b.first > 0 && a.indent != b.indent:
		return fmt.Errorf("block at line %d has content indent %d; stripping its first line would change it to %d", b.header, b.indent, a.indent)
	}
	return nil
}

func blockIndentsSame(before, after []byte) error {
	_, beforeBlocks, err := yamlScan(before)
	if err != nil {
		return fmt.Errorf("scan original: %w", err)
	}
	_, afterBlocks, err := yamlScan(after)
	if err != nil {
		return fmt.Errorf("scan stripped: %w", err)
	}
	if len(beforeBlocks) != len(afterBlocks) {
		return fmt.Errorf("stripped YAML has %d block scalars, source has %d", len(afterBlocks), len(beforeBlocks))
	}
	for i, b := range beforeBlocks {
		if err := blockIndentKept(b, afterBlocks[i]); err != nil {
			return err
		}
	}
	return nil
}

func yamlTreesSame(before, after []byte, workflow bool) error {
	beforeDocs, err := decodeYamlDocs(before)
	if err != nil {
		return fmt.Errorf("decode original: %w", err)
	}
	afterDocs, err := decodeYamlDocs(after)
	if err != nil {
		return fmt.Errorf("decode stripped: %w", err)
	}
	if len(beforeDocs) != len(afterDocs) {
		return fmt.Errorf("stripped YAML has %d documents, source has %d", len(afterDocs), len(beforeDocs))
	}
	cmp := treeComparison{workflow: workflow}
	for i := range beforeDocs {
		clearComments(beforeDocs[i])
		if line := firstComment(afterDocs[i]); line > 0 {
			return fmt.Errorf("comment survived strip at line %d", line)
		}
		if err := cmp.same(beforeDocs[i], afterDocs[i], noStepContext, false); err != nil {
			return err
		}
	}
	return nil
}

func clearComments(n *yaml.Node) {
	n.HeadComment, n.LineComment, n.FootComment = "", "", ""
	for _, c := range n.Content {
		clearComments(c)
	}
}

func firstComment(n *yaml.Node) int {
	if n.HeadComment != "" || n.LineComment != "" || n.FootComment != "" {
		return max(n.Line, 1)
	}
	for _, c := range n.Content {
		if line := firstComment(c); line > 0 {
			return line
		}
	}
	return 0
}

type treeComparison struct{ workflow bool }

func (tc treeComparison) same(b, a *yaml.Node, ctx stepContext, shellRun bool) error {
	if b.Kind != a.Kind || b.Tag != a.Tag || b.Style != a.Style || b.Anchor != a.Anchor ||
		len(b.Content) != len(a.Content) {
		return fmt.Errorf("stripped YAML differs from the source at line %d", b.Line)
	}
	if shellRun {
		return runValuesSame(b, a)
	}
	if b.Value != a.Value {
		return fmt.Errorf("stripped YAML value differs from the source at line %d", b.Line)
	}
	for i := range b.Content {
		bc, ac := b.Content[i], a.Content[i]
		ctxChild, isRun := tc.childContext(b, i, ctx)
		if err := tc.same(bc, ac, ctxChild, isRun); err != nil {
			return err
		}
	}
	return nil
}

func (tc treeComparison) childContext(n *yaml.Node, i int, ctx stepContext) (stepContext, bool) {
	if !tc.workflow {
		return noStepContext, false
	}
	child := n.Content[i]
	if n.Kind != yaml.MappingNode {
		return childStepContext(n, ctx), false
	}
	if i%2 == 0 {
		return noStepContext, false
	}
	key := n.Content[i-1].Value
	if ctx == stepMapping && key == "run" {
		return noStepContext, child.Kind == yaml.ScalarNode && child.Style&yaml.LiteralStyle != 0
	}
	if key == "steps" && child.Kind == yaml.SequenceNode {
		return stepsSequence, false
	}
	return noStepContext, false
}

func runValuesSame(b, a *yaml.Node) error {
	beforeText, err := maskExpressions([]byte(b.Value))
	if err != nil {
		return err
	}
	afterText, err := maskExpressions([]byte(a.Value))
	if err != nil {
		return err
	}
	if err := shellCommentFreePrintsEqual(beforeText, afterText); err != nil {
		return fmt.Errorf("run: block at line %d: %w", b.Line, err)
	}
	hits, err := shellCommentHits([]byte(a.Value))
	if err != nil {
		return fmt.Errorf("run: block at line %d: %w", a.Line, err)
	}
	for _, h := range hits {
		if h.whole {
			return fmt.Errorf("run: block at line %d: whole-line comment survived strip", a.Line)
		}
	}
	return nil
}
