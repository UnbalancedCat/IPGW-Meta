// Package doccheck validates documentation authority and its generated stable-ID index.
package doccheck

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	PlanID    = "IPGW-META-V1"
	Revision  = "2026-08-28-r2"
	IndexPath = "docs/stable-ids.md"
)

var requiredPaths = []string{
	"AGENTS.md",
	"docs/README.md",
	"docs/upgrade/plan.md",
	"docs/upgrade/status.md",
	"docs/upgrade/migration-matrix.md",
	"docs/research/active-implementations.md",
	"docs/architecture/overview.md",
	"docs/architecture/protocol-correctness.md",
	"docs/architecture/security.md",
	"docs/architecture/decisions/README.md",
	"docs/architecture/decisions/ADR-0007-immutable-candidate-promotion.md",
	"docs/architecture/decisions/ADR-0008-offline-transactional-installer.md",
	"docs/architecture/decisions/ADR-0009-separated-live-test-plane.md",
	"docs/architecture/decisions/ADR-0011-nonblocking-m0-governance.md",
	"docs/compatibility/auth-capabilities.md",
	"docs/runbooks/headless-auth.md",
	"docs/runbooks/campus-lab.md",
	"docs/reference/cli.md",
	"docs/reference/go-sdk.md",
	"docs/reference/json-cli.md",
	"docs/operations/config-migration.md",
	"docs/operations/release.md",
	"docs/operations/offline-install.md",
	"docs/operations/live-validation.md",
	"docs/evidence/README.md",
	"agent/README.md",
	"agent/plans/stabilization-v1.md",
	"agent/handoff.md",
}

const stableIDPattern = `(?:[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+-[0-9]{3}|ADR-[0-9]{4})`

var (
	stableID          = regexp.MustCompile(`\b` + stableIDPattern + `\b`)
	declarationPrefix = regexp.MustCompile(`^(` + stableIDPattern + `)(?:[ \t]|：|:|$)`)
	headingLine       = regexp.MustCompile(`^[ \t]{0,3}(#{1,6})[ \t]+(.+?)[ \t]*$`)
	closingHashes     = regexp.MustCompile(`[ \t]+#+[ \t]*$`)
	markdownLink      = regexp.MustCompile(`!?\[([^\]\n]*)\]\(([^)\n]*)\)`)
	inlineLink        = regexp.MustCompile(`!?\[([^\]\n]*)\]\([^)\n]*\)`)
	htmlTag           = regexp.MustCompile(`<[^>]+>`)
	explicitAnchor    = regexp.MustCompile(`(?i)<[a-z][^>]*(?:id|name)[ \t]*=[ \t]*["']([^"']+)["'][^>]*>`)
)

// Violation is one deterministic doccheck failure.
type Violation struct {
	Path    string
	Message string
}

func (v Violation) String() string {
	if v.Path == "" {
		return v.Message
	}
	return v.Path + ": " + v.Message
}

type heading struct {
	text   string
	anchor string
	line   int
}

type link struct {
	labelStart int
	labelEnd   int
	destStart  int
	destEnd    int
	target     string
	line       int
}

type document struct {
	path     string
	body     string
	headings []heading
	links    []link
}

type declaration struct {
	id     string
	title  string
	path   string
	anchor string
	line   int
}

type analysis struct {
	root         string
	documents    map[string]*document
	declarations map[string]declaration
	violations   []Violation
}

type resolvedTarget struct {
	path     string
	fragment string
	external bool
	exists   bool
}

// Check validates source documentation and compares the checked-in generated index.
// It never writes to the repository.
func Check(root string) ([]Violation, error) {
	result, err := analyze(root, false)
	if err != nil {
		return nil, err
	}
	expected := renderIndex(result.declarations)
	actual, readErr := os.ReadFile(filepath.Join(result.root, filepath.FromSlash(IndexPath)))
	switch {
	case os.IsNotExist(readErr):
		result.violations = append(result.violations, Violation{IndexPath, "generated stable-ID index is missing; run doccheck without --check"})
	case readErr != nil:
		return nil, fmt.Errorf("read %s: %w", IndexPath, readErr)
	case !bytes.Equal(normalizeNewlines(actual), expected):
		result.violations = append(result.violations, Violation{IndexPath, "generated stable-ID index is stale; run doccheck without --check"})
	}
	sortViolations(result.violations)
	return result.violations, nil
}

// Generate validates source documentation and writes the deterministic stable-ID
// index. It returns without writing when source violations exist.
func Generate(root string) (changed bool, violations []Violation, err error) {
	result, err := analyze(root, true)
	if err != nil {
		return false, nil, err
	}
	if len(result.violations) != 0 {
		sortViolations(result.violations)
		return false, result.violations, nil
	}

	expected := renderIndex(result.declarations)
	indexFile := filepath.Join(result.root, filepath.FromSlash(IndexPath))
	actual, readErr := os.ReadFile(indexFile)
	if readErr == nil && bytes.Equal(normalizeNewlines(actual), expected) {
		return false, nil, nil
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, nil, fmt.Errorf("read %s: %w", IndexPath, readErr)
	}
	if err := os.MkdirAll(filepath.Dir(indexFile), 0o755); err != nil {
		return false, nil, fmt.Errorf("create index directory: %w", err)
	}
	if err := os.WriteFile(indexFile, expected, 0o644); err != nil {
		return false, nil, fmt.Errorf("write %s: %w", IndexPath, err)
	}
	return true, nil, nil
}

func analyze(root string, allowMissingIndex bool) (*analysis, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)
	result := &analysis{
		root:         rootAbs,
		documents:    make(map[string]*document),
		declarations: make(map[string]declaration),
	}

	for _, name := range requiredPaths {
		info, statErr := os.Stat(filepath.Join(rootAbs, filepath.FromSlash(name)))
		switch {
		case os.IsNotExist(statErr):
			result.violations = append(result.violations, Violation{name, "required file is missing"})
		case statErr != nil:
			return nil, fmt.Errorf("stat %s: %w", name, statErr)
		case info.IsDir():
			result.violations = append(result.violations, Violation{name, "required path is a directory"})
		}
	}

	var paths []string
	for _, subtree := range []string{"docs", "agent"} {
		base := filepath.Join(rootAbs, subtree)
		walkErr := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return nil
				}
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "_site" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(path), ".md") {
				return nil
			}
			rel, relErr := filepath.Rel(rootAbs, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if rel != IndexPath {
				paths = append(paths, rel)
			}
			return nil
		})
		if walkErr != nil && !os.IsNotExist(walkErr) {
			return nil, fmt.Errorf("walk %s: %w", subtree, walkErr)
		}
	}
	if info, statErr := os.Stat(filepath.Join(rootAbs, "AGENTS.md")); statErr == nil && !info.IsDir() {
		paths = append(paths, "AGENTS.md")
	}
	sort.Strings(paths)

	for _, path := range paths {
		data, readErr := os.ReadFile(filepath.Join(rootAbs, filepath.FromSlash(path)))
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", path, readErr)
		}
		body := string(data)
		result.documents[path] = &document{
			path:     path,
			body:     body,
			headings: parseHeadings(body),
			links:    parseLinks(body),
		}
	}

	for _, path := range sortedDocumentPaths(result.documents) {
		doc := result.documents[path]
		if strings.HasPrefix(path, "docs/") || strings.HasPrefix(path, "agent/") {
			metadata := frontMatter(doc.body)
			if metadata["plan_id"] != PlanID {
				result.violations = append(result.violations, Violation{path, "front matter plan_id must be " + PlanID})
			}
			if metadata["revision"] != Revision {
				result.violations = append(result.violations, Violation{path, "front matter revision must be " + Revision})
			}
		}

		for _, item := range doc.headings {
			match := declarationPrefix.FindStringSubmatchIndex(item.text)
			if match == nil {
				continue
			}
			id := item.text[match[2]:match[3]]
			if !strings.HasPrefix(path, "docs/") {
				result.violations = append(result.violations, lineViolation(path, item.line, "stable ID "+id+" may only be declared by a docs/ heading"))
				continue
			}
			title := strings.TrimLeftFunc(item.text[match[3]:], func(r rune) bool {
				return unicode.IsSpace(r) || r == ':' || r == '：'
			})
			if title == "" {
				title = id
			}
			current := declaration{id: id, title: stripHeadingMarkup(title), path: path, anchor: item.anchor, line: item.line}
			if previous, exists := result.declarations[id]; exists {
				result.violations = append(result.violations, lineViolation(path, item.line,
					fmt.Sprintf("stable ID %s is already declared by %s:%d", id, previous.path, previous.line)))
				continue
			}
			result.declarations[id] = current
		}
	}

	for _, path := range sortedDocumentPaths(result.documents) {
		doc := result.documents[path]
		for _, item := range doc.links {
			resolved, issue := resolveTarget(rootAbs, path, item.target)
			if issue != "" {
				if allowMissingIndex && resolved.path == IndexPath && strings.Contains(issue, "does not exist") {
					continue
				}
				result.violations = append(result.violations, lineViolation(path, item.line, issue))
				continue
			}
			if resolved.external || resolved.fragment == "" || !resolved.exists {
				continue
			}
			anchors, anchorErr := anchorsForTarget(rootAbs, resolved.path, result.documents)
			if anchorErr != nil {
				return nil, anchorErr
			}
			if _, exists := anchors[resolved.fragment]; !exists {
				result.violations = append(result.violations, lineViolation(path, item.line,
					fmt.Sprintf("relative link fragment #%s does not exist in %s", resolved.fragment, resolved.path)))
			}
		}
	}

	for _, path := range sortedDocumentPaths(result.documents) {
		doc := result.documents[path]
		isAgent := path == "AGENTS.md" || strings.HasPrefix(path, "agent/")
		for _, location := range stableID.FindAllStringIndex(doc.body, -1) {
			id := doc.body[location[0]:location[1]]
			contextLink, contextKind := linkAt(doc.links, location[0], location[1])
			if contextKind == "destination" {
				continue
			}
			owner, known := result.declarations[id]
			if !known {
				result.violations = append(result.violations, lineViolation(path, lineNumber(doc.body, location[0]), "references undeclared docs ID "+id))
				continue
			}
			if contextKind == "label" {
				resolved, issue := resolveTarget(rootAbs, path, contextLink.target)
				if issue == "" && (resolved.external || resolved.path != owner.path) {
					result.violations = append(result.violations, lineViolation(path, contextLink.line,
						fmt.Sprintf("stable ID %s link must target its owner %s", id, owner.path)))
				}
				continue
			}
			if isAgent {
				result.violations = append(result.violations, lineViolation(path, lineNumber(doc.body, location[0]),
					fmt.Sprintf("agent reference %s must be a link to %s", id, owner.path)))
			}
		}
	}

	sortViolations(result.violations)
	return result, nil
}

func parseHeadings(body string) []heading {
	lines := strings.Split(body, "\n")
	counts := make(map[string]int)
	var result []heading
	var fence rune
	var fenceWidth int
	for index, rawLine := range lines {
		line := strings.TrimSuffix(rawLine, "\r")
		if marker, width, ok := fenceMarker(line); ok {
			if fence == 0 {
				fence, fenceWidth = marker, width
			} else if marker == fence && width >= fenceWidth {
				fence, fenceWidth = 0, 0
			}
			continue
		}
		if fence != 0 {
			continue
		}
		match := headingLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		text := strings.TrimSpace(closingHashes.ReplaceAllString(match[2], ""))
		base := githubSlug(text)
		anchor := base
		if count := counts[base]; count != 0 {
			anchor = fmt.Sprintf("%s-%d", base, count)
		}
		counts[base]++
		result = append(result, heading{text: text, anchor: anchor, line: index + 1})
	}
	return result
}

func fenceMarker(line string) (rune, int, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(line)-len(trimmed) > 3 || trimmed == "" {
		return 0, 0, false
	}
	marker := rune(trimmed[0])
	if marker != '`' && marker != '~' {
		return 0, 0, false
	}
	width := 0
	for _, r := range trimmed {
		if r != marker {
			break
		}
		width++
	}
	return marker, width, width >= 3
}

func githubSlug(text string) string {
	text = stripHeadingMarkup(text)
	var builder strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_' || r == '-':
			builder.WriteRune(r)
			lastHyphen = r == '-'
		case unicode.IsSpace(r):
			if builder.Len() != 0 && !lastHyphen {
				builder.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func stripHeadingMarkup(text string) string {
	text = inlineLink.ReplaceAllString(text, "$1")
	text = htmlTag.ReplaceAllString(text, "")
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		switch r {
		case '`', '*', '~':
			return -1
		default:
			return r
		}
	}, text))
}

func parseLinks(body string) []link {
	var result []link
	for _, match := range markdownLink.FindAllStringSubmatchIndex(body, -1) {
		if len(match) < 6 {
			continue
		}
		target, ok := markdownDestination(body[match[4]:match[5]])
		if !ok {
			target = strings.TrimSpace(body[match[4]:match[5]])
		}
		result = append(result, link{
			labelStart: match[2],
			labelEnd:   match[3],
			destStart:  match[4],
			destEnd:    match[5],
			target:     target,
			line:       lineNumber(body, match[0]),
		})
	}
	return result
}

func markdownDestination(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if strings.HasPrefix(raw, "<") {
		if end := strings.IndexByte(raw, '>'); end > 0 {
			return raw[1:end], true
		}
		return "", false
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", false
	}
	return fields[0], true
}

func resolveTarget(root, source, target string) (resolvedTarget, string) {
	parsed, err := url.Parse(target)
	if err != nil {
		return resolvedTarget{}, "invalid link target " + target
	}
	if parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(target, "//") {
		return resolvedTarget{external: true}, ""
	}

	var resolvedPath string
	switch {
	case parsed.Path == "":
		resolvedPath = filepath.Join(root, filepath.FromSlash(source))
	case strings.HasPrefix(parsed.Path, "/"):
		resolvedPath = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(parsed.Path, "/")))
	default:
		resolvedPath = filepath.Join(root, filepath.Dir(filepath.FromSlash(source)), filepath.FromSlash(parsed.Path))
	}
	resolvedPath = filepath.Clean(resolvedPath)
	rel, err := filepath.Rel(root, resolvedPath)
	if err != nil {
		return resolvedTarget{}, "cannot resolve relative link " + target
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return resolvedTarget{path: filepath.ToSlash(rel), fragment: parsed.Fragment}, "relative link escapes repository: " + target
	}
	relSlash := filepath.ToSlash(rel)
	info, statErr := os.Stat(resolvedPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return resolvedTarget{path: relSlash, fragment: parsed.Fragment}, "relative link target does not exist: " + target
		}
		return resolvedTarget{}, "cannot stat relative link target " + target
	}
	if info.IsDir() && parsed.Fragment != "" {
		return resolvedTarget{path: relSlash, fragment: parsed.Fragment, exists: true}, "relative link fragment targets a directory: " + target
	}
	return resolvedTarget{path: relSlash, fragment: parsed.Fragment, exists: true}, ""
}

func anchorsForTarget(root, path string, documents map[string]*document) (map[string]struct{}, error) {
	var body string
	var headings []heading
	if doc, ok := documents[path]; ok {
		body, headings = doc.body, doc.headings
	} else {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("read anchor target %s: %w", path, err)
		}
		body, headings = string(data), parseHeadings(string(data))
	}
	anchors := make(map[string]struct{})
	for _, item := range headings {
		if item.anchor != "" {
			anchors[item.anchor] = struct{}{}
		}
	}
	for _, match := range explicitAnchor.FindAllStringSubmatch(body, -1) {
		anchors[match[1]] = struct{}{}
	}
	return anchors, nil
}

func linkAt(links []link, start, end int) (link, string) {
	for _, item := range links {
		if start >= item.labelStart && end <= item.labelEnd {
			return item, "label"
		}
		if start >= item.destStart && end <= item.destEnd {
			return item, "destination"
		}
	}
	return link{}, ""
}

func renderIndex(declarations map[string]declaration) []byte {
	ids := make([]string, 0, len(declarations))
	for id := range declarations {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("plan_id: " + PlanID + "\n")
	builder.WriteString("revision: " + Revision + "\n")
	builder.WriteString("generated: true\n")
	builder.WriteString("---\n\n")
	builder.WriteString("# 稳定 ID 索引\n\n")
	builder.WriteString("本文件由 `doccheck` 根据 `docs/` 中的权威标题确定性生成，请勿手工编辑。\n\n")
	builder.WriteString("| 稳定 ID | 标题 | 权威声明 |\n")
	builder.WriteString("|---|---|---|\n")
	for _, id := range ids {
		item := declarations[id]
		relative := strings.TrimPrefix(item.path, "docs/")
		target := relative
		if item.anchor != "" {
			target += "#" + item.anchor
		}
		fmt.Fprintf(&builder, "| `%s` | %s | [%s](%s) |\n",
			id, escapeTableCell(item.title), escapeTableCell(item.path), target)
	}
	return []byte(builder.String())
}

func escapeTableCell(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "|", `\|`)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func normalizeNewlines(data []byte) []byte {
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

func frontMatter(body string) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(body))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return result
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			result[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return result
}

func sortedDocumentPaths(documents map[string]*document) []string {
	paths := make([]string, 0, len(documents))
	for path := range documents {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func lineNumber(body string, offset int) int {
	if offset < 0 {
		return 1
	}
	if offset > len(body) {
		offset = len(body)
	}
	return strings.Count(body[:offset], "\n") + 1
}

func lineViolation(path string, line int, message string) Violation {
	return Violation{Path: path, Message: fmt.Sprintf("line %d: %s", line, message)}
}

func sortViolations(violations []Violation) {
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Path == violations[j].Path {
			return violations[i].Message < violations[j].Message
		}
		return violations[i].Path < violations[j].Path
	})
}
