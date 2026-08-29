package livegate

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxCandidateJSONBytes = 64 * 1024
	MaxCandidateJSONDepth = 64
)

// ErrInvalidCandidateJSON is returned for every rejected candidate JSON
// document. It deliberately carries no input-derived detail.
var ErrInvalidCandidateJSON = errors.New("livegate: invalid candidate JSON")

var errInvalidCandidateUTF8 = errors.New("invalid candidate UTF-8")

type candidateJSONNodeKind uint8

const (
	candidateJSONScalar candidateJSONNodeKind = iota
	candidateJSONObject
	candidateJSONArray
)

type candidateJSONNode struct {
	kind   candidateJSONNodeKind
	scalar any
}

type candidateJSONProjector func(path []string, node candidateJSONNode) error

// ProjectCandidateJSON walks one candidate JSON value without retaining its
// raw representation. The public projection callback remains scalar-only.
func ProjectCandidateJSON(r io.Reader, project func(path []string, scalar any) error) error {
	if project == nil {
		return projectCandidateJSONNodes(r, nil)
	}
	return projectCandidateJSONNodes(r, func(path []string, node candidateJSONNode) error {
		if node.kind != candidateJSONScalar {
			return nil
		}
		return project(path, node.scalar)
	})
}

// projectCandidateJSONNodes additionally reports object and array nodes at
// their exact segment paths. This lets trusted internal consumers validate
// empty containers and field types without retaining candidate bytes.
func projectCandidateJSONNodes(r io.Reader, project candidateJSONProjector) error {
	if r == nil {
		return ErrInvalidCandidateJSON
	}

	limited := &io.LimitedReader{R: r, N: MaxCandidateJSONBytes + 1}
	decoder := json.NewDecoder(newCandidateUTF8Reader(limited))
	decoder.UseNumber()

	if err := projectCandidateJSONValue(decoder, nil, 0, project); err != nil {
		return ErrInvalidCandidateJSON
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalidCandidateJSON
	}
	if limited.N == 0 {
		return ErrInvalidCandidateJSON
	}
	return nil
}

func projectCandidateJSONValue(
	decoder *json.Decoder,
	path []string,
	depth int,
	project candidateJSONProjector,
) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidCandidateJSON
	}

	delim, container := token.(json.Delim)
	if !container {
		if value, ok := token.(string); ok && strings.ContainsRune(value, utf8.RuneError) {
			return ErrInvalidCandidateJSON
		}
		return projectCandidateJSONNode(path, candidateJSONNode{
			kind:   candidateJSONScalar,
			scalar: token,
		}, project)
	}

	if depth >= MaxCandidateJSONDepth {
		return ErrInvalidCandidateJSON
	}
	switch delim {
	case '{':
		if err := projectCandidateJSONNode(path, candidateJSONNode{kind: candidateJSONObject}, project); err != nil {
			return err
		}
		return projectCandidateJSONObject(decoder, path, depth+1, project)
	case '[':
		if err := projectCandidateJSONNode(path, candidateJSONNode{kind: candidateJSONArray}, project); err != nil {
			return err
		}
		return projectCandidateJSONArray(decoder, path, depth+1, project)
	default:
		return ErrInvalidCandidateJSON
	}
}

func projectCandidateJSONNode(
	path []string,
	node candidateJSONNode,
	project candidateJSONProjector,
) error {
	if project == nil {
		return nil
	}
	pathCopy := append([]string(nil), path...)
	if err := project(pathCopy, node); err != nil {
		return ErrInvalidCandidateJSON
	}
	return nil
}

func projectCandidateJSONObject(
	decoder *json.Decoder,
	path []string,
	depth int,
	project candidateJSONProjector,
) error {
	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return ErrInvalidCandidateJSON
		}
		key, ok := keyToken.(string)
		if !ok || strings.ContainsRune(key, utf8.RuneError) {
			return ErrInvalidCandidateJSON
		}
		if _, duplicate := seen[key]; duplicate {
			return ErrInvalidCandidateJSON
		}
		seen[key] = struct{}{}

		childPath := append(path, key)
		if err := projectCandidateJSONValue(decoder, childPath, depth, project); err != nil {
			return ErrInvalidCandidateJSON
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return ErrInvalidCandidateJSON
	}
	return nil
}

func projectCandidateJSONArray(
	decoder *json.Decoder,
	path []string,
	depth int,
	project candidateJSONProjector,
) error {
	index := 0
	for decoder.More() {
		childPath := append(path, strconv.Itoa(index))
		if err := projectCandidateJSONValue(decoder, childPath, depth, project); err != nil {
			return ErrInvalidCandidateJSON
		}
		index++
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim(']') {
		return ErrInvalidCandidateJSON
	}
	return nil
}

// candidateUTF8Reader validates the byte stream incrementally. encoding/json
// otherwise replaces invalid UTF-8 and invalid surrogate strings with
// utf8.RuneError, so decoded keys and scalar strings are checked as well.
type candidateUTF8Reader struct {
	reader      *bufio.Reader
	pending     []byte
	terminalErr error
}

func newCandidateUTF8Reader(r io.Reader) *candidateUTF8Reader {
	return &candidateUTF8Reader{
		reader:  bufio.NewReader(r),
		pending: make([]byte, 0, utf8.UTFMax),
	}
}

func (r *candidateUTF8Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	written := 0
	for written < len(p) {
		if len(r.pending) != 0 {
			n := copy(p[written:], r.pending)
			written += n
			r.pending = r.pending[n:]
			continue
		}

		if r.terminalErr != nil {
			if written != 0 {
				return written, nil
			}
			return 0, r.terminalErr
		}

		runeValue, size, err := r.reader.ReadRune()
		if err != nil {
			r.terminalErr = err
			if written != 0 {
				return written, nil
			}
			return 0, err
		}
		if runeValue == utf8.RuneError && size == 1 {
			r.terminalErr = errInvalidCandidateUTF8
			if written != 0 {
				return written, nil
			}
			return 0, r.terminalErr
		}
		r.pending = utf8.AppendRune(r.pending[:0], runeValue)
	}
	return written, nil
}
