package utask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

type Store struct {
	nc      *nats.Conn
	js      nats.JetStreamContext
	tasksKV nats.KeyValue
	tagsKV  nats.KeyValue
	ns      string
}

func bucketNames(ns string) (tasks, tags string) {
	// NATS KV bucket names cannot contain dots. Use underscore + suffix by namespace.
	// Examples: utask_tasks_default, utask_tags_default
	return fmt.Sprintf("utask_tasks_%s", ns), fmt.Sprintf("utask_tags_%s", ns)
}

// Open connects to NATS, ensures KV buckets for the namespace, and returns a Store.
func Open(ctx context.Context, url, namespace string) (*Store, error) {
	if namespace == "" {
		namespace = "default"
	}
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	tasksName, tagsName := bucketNames(namespace)

	// Ensure KV buckets
	tasksKV, err := js.KeyValue(tasksName)
	if err != nil {
		if errors.Is(err, nats.ErrBucketNotFound) {
			tasksKV, err = js.CreateKeyValue(&nats.KeyValueConfig{Bucket: tasksName})
		}
		if err != nil {
			nc.Close()
			return nil, fmt.Errorf("ensure tasks bucket: %w", err)
		}
	}
	tagsKV, err := js.KeyValue(tagsName)
	if err != nil {
		if errors.Is(err, nats.ErrBucketNotFound) {
			tagsKV, err = js.CreateKeyValue(&nats.KeyValueConfig{Bucket: tagsName})
		}
		if err != nil {
			nc.Close()
			return nil, fmt.Errorf("ensure tags bucket: %w", err)
		}
	}

	s := &Store{nc: nc, js: js, tasksKV: tasksKV, tagsKV: tagsKV, ns: namespace}
	return s, nil
}

func (s *Store) Close() { s.nc.Drain(); s.nc.Close() }

// CreateTask creates a task idempotently. Returns the task and whether it already existed.
func (s *Store) CreateTask(ctx context.Context, in TaskInput) (Task, bool, error) {
	c, id := NormalizeInput(in)
	now := time.Now().UTC()
	t := Task{
		ID:              id,
		Text:            c.Text,
		Done:            false,
		Created:         now.Format(time.RFC3339),
		Tags:            c.Tags,
		Priority:        c.Priority,
		EstimateMinutes: c.EstimateMinutes,
	}
	b, _ := json.Marshal(t)

	// Create only if not exists
	if _, err := s.tasksKV.Create(id, b); err != nil {
		if errors.Is(err, nats.ErrKeyExists) {
			// Fetch existing
			e, gerr := s.tasksKV.Get(id)
			if gerr != nil {
				return Task{}, false, fmt.Errorf("get existing: %w", gerr)
			}
			var existing Task
			if jerr := json.Unmarshal(e.Value(), &existing); jerr != nil {
				return Task{}, false, fmt.Errorf("decode existing: %w", jerr)
			}
			return existing, true, nil
		}
		return Task{}, false, fmt.Errorf("create task: %w", err)
	}

	// Update tag index
	for _, tag := range t.Tags {
		if err := s.appendTagID(tag, t.ID); err != nil {
			return Task{}, false, err
		}
	}

    // Events removed

	return t, false, nil
}

func (s *Store) appendTagID(tag, id string) error {
	// Try update existing with CAS
	e, err := s.tagsKV.Get(tag)
	if err != nil {
		if errors.Is(err, nats.ErrKeyNotFound) {
			// Create new
			v := id
			if _, err := s.tagsKV.Create(tag, []byte(v)); err != nil && !errors.Is(err, nats.ErrKeyExists) {
				return fmt.Errorf("create tag index: %w", err)
			}
			if errors.Is(err, nats.ErrKeyExists) {
				// Race: fall through to update path
				return s.appendTagID(tag, id)
			}
			return nil
		}
		return fmt.Errorf("get tag index: %w", err)
	}
	// Parse existing
	lines := strings.Split(string(e.Value()), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == id {
			return nil // already present
		}
	}
	lines = append(lines, id)
	newVal := strings.TrimSpace(strings.Join(lines, "\n"))
	if _, err := s.tagsKV.Update(tag, []byte(newVal), e.Revision()); err != nil {
		return fmt.Errorf("update tag index: %w", err)
	}
	return nil
}

func (s *Store) removeTagID(tag, id string) error {
	e, err := s.tagsKV.Get(tag)
	if err != nil {
		if errors.Is(err, nats.ErrKeyNotFound) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(e.Value()), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == id || strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(line))
	}
	newVal := strings.TrimSpace(strings.Join(out, "\n"))
	if _, err := s.tagsKV.Update(tag, []byte(newVal), e.Revision()); err != nil {
		return err
	}
	return nil
}

func (s *Store) GetTask(ctx context.Context, id string) (Task, uint64, error) {
	e, err := s.tasksKV.Get(id)
	if err != nil {
		if errors.Is(err, nats.ErrKeyNotFound) {
			return Task{}, 0, fmt.Errorf("not found")
		}
		return Task{}, 0, err
	}
	var t Task
	if err := json.Unmarshal(e.Value(), &t); err != nil {
		return Task{}, 0, err
	}
	return t, e.Revision(), nil
}

func (s *Store) putTaskCAS(id string, t Task, rev uint64) error {
	b, _ := json.Marshal(t)
	if _, err := s.tasksKV.Put(id, b); err != nil {
		return err
	}
	return nil
}

// UpdateTask modifies fields and updates the tag index.
func (s *Store) UpdateTask(ctx context.Context, id string, set UpdateSet) (Task, error) {
	before, rev, err := s.GetTask(ctx, id)
	if err != nil {
		return Task{}, err
	}
	after := before
	if set.Text != nil {
		after.Text = strings.TrimSpace(*set.Text)
	}
	if set.Done != nil {
		after.Done = *set.Done
	}
	if set.Tags != nil {
		// normalize tags
		seen := map[string]struct{}{}
		norm := make([]string, 0, len(*set.Tags))
		for _, t := range *set.Tags {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			norm = append(norm, t)
		}
		after.Tags = norm
	}
	if set.Priority != nil {
		after.Priority = *set.Priority
	}
	if err := s.putTaskCAS(id, after, rev); err != nil {
		return Task{}, err
	}
	// Tag diff
	beforeSet := map[string]struct{}{}
	afterSet := map[string]struct{}{}
	for _, t := range before.Tags {
		beforeSet[t] = struct{}{}
	}
	for _, t := range after.Tags {
		afterSet[t] = struct{}{}
	}
	for t := range afterSet {
		if _, ok := beforeSet[t]; !ok {
			_ = s.appendTagID(t, id)
		}
	}
	for t := range beforeSet {
		if _, ok := afterSet[t]; !ok {
			_ = s.removeTagID(t, id)
		}
	}
    // Events removed
    return after, nil
}

// DeleteTask removes a task and its tag references.
func (s *Store) DeleteTask(ctx context.Context, id string) (string, error) {
	t, _, err := s.GetTask(ctx, id)
	if err != nil {
		return "", err
	}
	if err := s.tasksKV.Delete(id); err != nil {
		return "", err
	}
	for _, tag := range t.Tags {
		_ = s.removeTagID(tag, id)
	}
    // Events removed
    return t.ID, nil
}

func (s *Store) CloseTask(ctx context.Context, id string) (Task, bool, error) {
	t, rev, err := s.GetTask(ctx, id)
	if err != nil {
		return Task{}, false, err
	}
	if t.Done {
		return t, false, nil
	}
	t.Done = true
	if err := s.putTaskCAS(id, t, rev); err != nil {
		return Task{}, false, err
	}
    // Events removed
    return t, true, nil
}

func (s *Store) ReopenTask(ctx context.Context, id string) (Task, bool, error) {
	t, rev, err := s.GetTask(ctx, id)
	if err != nil {
		return Task{}, false, err
	}
	if !t.Done {
		return t, false, nil
	}
	t.Done = false
	if err := s.putTaskCAS(id, t, rev); err != nil {
		return Task{}, false, err
	}
    // Events removed
    return t, true, nil
}

// List tasks; if tag is non-empty, list by tag index, else scan all keys.
func (s *Store) List(ctx context.Context, tag string, statusFilter Status) ([]Task, error) {
	out := []Task{}
	if tag != "" {
		e, err := s.tagsKV.Get(strings.ToLower(strings.TrimSpace(tag)))
		if err != nil {
			if errors.Is(err, nats.ErrKeyNotFound) {
				return out, nil
			}
			return nil, err
		}
		ids := strings.Split(string(e.Value()), "\n")
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			t, _, err := s.GetTask(ctx, id)
			if err != nil {
				continue
			}
			if statusFilter != "" {
				if statusFilter == StatusOpen && t.Done {
					continue
				}
				if statusFilter == StatusClosed && !t.Done {
					continue
				}
			}
			out = append(out, t)
		}
		return out, nil
	}
	// Scan all entries in tasks bucket
	keys, err := s.tasksKV.Keys()
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if k == "" {
			continue
		}
		t, _, err := s.GetTask(ctx, k)
		if err != nil {
			continue
		}
		if statusFilter != "" {
			if statusFilter == StatusOpen && t.Done {
				continue
			}
			if statusFilter == StatusClosed && !t.Done {
				continue
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// Query returns tasks matching ANY(allAny) union and ALL(allAll) intersection, with optional limit.
func (s *Store) Query(ctx context.Context, any, all []string, limit int) ([]Task, error) {
	norm := func(in []string) []string {
		out := make([]string, 0, len(in))
		seen := map[string]struct{}{}
		for _, t := range in {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
		return out
	}
	any = norm(any)
	all = norm(all)

	// Helper to fetch set of IDs for a tag
	readTag := func(tag string) (map[string]struct{}, error) {
		out := map[string]struct{}{}
		e, err := s.tagsKV.Get(tag)
		if err != nil {
			if errors.Is(err, nats.ErrKeyNotFound) {
				return out, nil
			}
			return nil, err
		}
		for _, line := range strings.Split(string(e.Value()), "\n") {
			id := strings.TrimSpace(line)
			if id != "" {
				out[id] = struct{}{}
			}
		}
		return out, nil
	}

	union := map[string]struct{}{}
	if len(any) == 0 {
		// If ANY not provided, start union with all task IDs
		keys, err := s.tasksKV.Keys()
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if k != "" {
				union[k] = struct{}{}
			}
		}
	} else {
		for _, tag := range any {
			ids, err := readTag(tag)
			if err != nil {
				return nil, err
			}
			for id := range ids {
				union[id] = struct{}{}
			}
		}
	}

	// Apply ALL intersection
	for _, tag := range all {
		ids, err := readTag(tag)
		if err != nil {
			return nil, err
		}
		// intersect union with ids
		for id := range union {
			if _, ok := ids[id]; !ok {
				delete(union, id)
			}
		}
	}

	// Fetch tasks
	out := []Task{}
	for id := range union {
		t, _, err := s.GetTask(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, t)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// RebuildIndex scans all tasks and rewrites the tag index from scratch.
func (s *Store) RebuildIndex(ctx context.Context) error {
	keys, err := s.tasksKV.Keys()
	if err != nil {
		return err
	}
	acc := map[string][]string{}
	for _, k := range keys {
		if k == "" {
			continue
		}
		t, _, err := s.GetTask(ctx, k)
		if err != nil {
			continue
		}
		for _, tag := range t.Tags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" {
				continue
			}
			acc[tag] = append(acc[tag], t.ID)
		}
	}
	// Delete old tags not present
	oldKeys, err := s.tagsKV.Keys()
	if err == nil {
		for _, ok := range oldKeys {
			if ok == "" {
				continue
			}
			if _, present := acc[ok]; !present {
				_ = s.tagsKV.Delete(ok)
			}
		}
	}
	// Write new values
	for tag, ids := range acc {
		val := strings.Join(ids, "\n")
		if _, err := s.tagsKV.Put(tag, []byte(val)); err != nil {
			return fmt.Errorf("write tag %s: %w", tag, err)
		}
	}
	return nil
}

// Events removed: no publish/subscribe helpers

// Resolve implements Git-style prefix resolution. Returns full id and candidates on ambiguity.
func (s *Store) Resolve(prefix string) (string, []string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", nil, fmt.Errorf("empty prefix")
	}
	// List keys via deprecated Keys(). Good enough for now.
	keys, err := s.tasksKV.Keys()
	if err != nil {
		return "", nil, err
	}
	return matchPrefix(keys, prefix)
}

// matchPrefix applies Git-style prefix resolution on a list of full IDs.
func matchPrefix(keys []string, prefix string) (string, []string, error) {
	matches := []string{}
	for _, k := range keys {
		if strings.HasPrefix(k, prefix) {
			matches = append(matches, k)
		}
	}
	switch len(matches) {
	case 0:
		return "", nil, fmt.Errorf("not found")
	case 1:
		return matches[0], nil, nil
	default:
		return "", matches, fmt.Errorf("ambiguous")
	}
}

// ListTags returns tag names with approximate counts based on index lines.
func (s *Store) ListTags() (map[string]int, error) {
	counts := map[string]int{}
	keys, err := s.tagsKV.Keys()
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if k == "" {
			continue
		}
		e, err := s.tagsKV.Get(k)
		if err != nil {
			continue
		}
		lines := strings.Split(string(e.Value()), "\n")
		n := 0
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				n++
			}
		}
		counts[k] = n
	}
	return counts, nil
}
