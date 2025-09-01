package utask

import "strings"

// Status is kept for filtering semantics in the CLI.
type Status string

const (
	StatusOpen   Status = "open"
	StatusClosed Status = "closed"
)

// Task matches the spec fields, with optional extended metadata.
type Task struct {
	ID              string   `json:"id"`
	Text            string   `json:"text"`
	Done            bool     `json:"done"`
	Tags            []string `json:"tags"`
	Created         string   `json:"created"`
	Priority        int      `json:"priority,omitempty"`
	EstimateMinutes int      `json:"estimate_minutes,omitempty"`
}

type TaskInput struct {
	Text            string
	Tags            []string
	Priority        int
	EstimateMinutes int
}

// UpdateSet describes allowed fields to modify in UpdateTask.
type UpdateSet struct {
	Text     *string
	Done     *bool
	Tags     *[]string
	Priority *int
}

// Trailer represents a parsed Git-like trailer "Key: Value".
type Trailer struct {
    Key   string
    Value string
}

// Short returns the first line of the task text, trimmed.
func (t Task) Short() string {
	s := t.Text
	if i := indexNL(s); i >= 0 {
		s = s[:i]
	}
	return trimSpace(s)
}

// Details returns the text following the first line, with leading/trailing
// blank lines trimmed.
func (t Task) Details() string {
    lines := splitLines(t.Text)
    if len(lines) <= 1 {
        return ""
    }
    end := len(lines)
    // Identify trailer block separated by at least one blank line
    if drops, trs, ok := t.trailerBlock(); ok && (len(trs) > 0 || len(drops) > 0) {
        // Cut body before the trailer block start
        _, start := t.trailerRegionBounds()
        end = start
    }
    body := joinLines(lines[1:end])
    return trimBlankLines(body)
}

// Trailers parses Git-like trailers from the end of the message.
// It returns trailers in display order (top-to-bottom of the trailer block).
func (t Task) Trailers() []Trailer {
    _, trailers, _ := t.trailerBlock()
    return trailers
}

// TrailerDrops returns the raw lines inside the trailer block that do not
// conform to the trailer "Key: Value" format and are therefore dropped.
func (t Task) TrailerDrops() []string {
    drops, _, _ := t.trailerBlock()
    return drops
}

// --- helpers (kept local to avoid extra deps) ---

func indexNL(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string { return strings.TrimSpace(s) }

func splitLines(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func trimBlankLines(s string) string {
	lines := splitLines(s)
	// trim leading blanks
	start := 0
	for start < len(lines) && trimSpace(lines[start]) == "" {
		start++
	}
	// trim trailing blanks
	end := len(lines)
	for end > start && trimSpace(lines[end-1]) == "" {
		end--
	}
	if start >= end {
		return ""
	}
	// join with \n
	n := 0
	for i := start; i < end; i++ {
		n += len(lines[i])
		if i+1 < end {
			n++
		}
	}
	b := make([]byte, 0, n)
	for i := start; i < end; i++ {
		b = append(b, lines[i]...)
		if i+1 < end {
			b = append(b, '\n')
		}
	}
	return string(b)
}

func joinLines(lines []string) string {
    if len(lines) == 0 {
        return ""
    }
    n := 0
    for i := 0; i < len(lines); i++ {
        n += len(lines[i])
        if i+1 < len(lines) {
            n++
        }
    }
    b := make([]byte, 0, n)
    for i := 0; i < len(lines); i++ {
        b = append(b, lines[i]...)
        if i+1 < len(lines) {
            b = append(b, '\n')
        }
    }
    return string(b)
}

func parseTrailer(s string) (Trailer, bool) {
	// Minimal parse: Key: Value, Key matches [A-Za-z0-9-]+
	// Find colon
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			key := s[:i]
			if !isValidKey(key) {
				return Trailer{}, false
			}
			// skip spaces after colon
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			return Trailer{Key: key, Value: s[j:]}, true
		}
	}
	return Trailer{}, false
}

func isValidKey(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return false
	}
	return true
}

// trailerRegionBounds returns (endIndexExclusive, startIndexInclusive) for the
// trailer region, if present, otherwise (len(lines), len(lines)).
func (t Task) trailerRegionBounds() (end, start int) {
    lines := splitLines(t.Text)
    n := len(lines)
    // Skip trailing blanks
    endIdx := n - 1
    for endIdx >= 0 && trimSpace(lines[endIdx]) == "" {
        endIdx--
    }
    if endIdx < 0 {
        return n, n
    }
    // Look upwards for the separating blank line(s)
    k := endIdx
    foundBlank := false
    for k >= 0 {
        if trimSpace(lines[k]) == "" {
            foundBlank = true
            break
        }
        k--
    }
    if !foundBlank || k == endIdx {
        // No separating blank line between body and tail block
        return n, n
    }
    // Trailer region starts after the last contiguous blank(s)
    startIdx := k + 1
    // Move startIdx forward if multiple blanks
    for startIdx < n && trimSpace(lines[startIdx]) == "" {
        startIdx++
    }
    return endIdx + 1, startIdx
}

// trailerBlock returns (drops, trailers, ok) where ok=true if a trailer region
// is present (separated by blank lines). Drops are lines in the region that do
// not match the trailer format and will be excluded from Details().
func (t Task) trailerBlock() (drops []string, trailers []Trailer, ok bool) {
    lines := splitLines(t.Text)
    end, start := t.trailerRegionBounds()
    if start >= end {
        return nil, nil, false
    }
    drops = []string{}
    trailers = []Trailer{}
    for i := start; i < end; i++ {
        line := lines[i]
        if kv, ok := parseTrailer(line); ok {
            trailers = append(trailers, kv)
        } else if trimSpace(line) != "" { // ignore extra blanks in region
            drops = append(drops, line)
        }
    }
    return drops, trailers, true
}
