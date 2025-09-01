package utask

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

var spaceRe = regexp.MustCompile(`\s+`)

type canonical struct {
	Text            string   `json:"text"`
	Tags            []string `json:"tags"`
	Priority        int      `json:"priority"`
	EstimateMinutes int      `json:"estimate_minutes"`
}

// NormalizeInput canonicalizes input for id derivation and returns the canonical
// form plus the derived id. IDs are a deterministic sha512 of the canonical JSON.
func NormalizeInput(in TaskInput) (canonical, string) {
    text := strings.TrimSpace(in.Text)

	// Normalize tags: lowercase, trim, drop empties, dedupe, sort
	seen := map[string]struct{}{}
	tags := make([]string, 0, len(in.Tags))
	for _, t := range in.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		tags = append(tags, t)
	}
	sort.Strings(tags)

	c := canonical{
		Text:            text,
		Tags:            tags,
		Priority:        in.Priority,
		EstimateMinutes: in.EstimateMinutes,
	}

	// Deterministic JSON via struct field order
	b, _ := json.Marshal(c)
	sum := sha512.Sum512(b)
	id := hex.EncodeToString(sum[:])
	return c, id
}
