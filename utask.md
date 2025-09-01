utask Technical Specification

Overview

utask is a minimal distributed task manager built on NATS/JetStream. It is designed for a tiny footprint and feature set, with all state stored in NATS key-value (KV) buckets. There is no central server beyond NATS itself. Clients (ut CLI, libraries) interact directly with KV buckets.

⸻

Data Model

Each task is a JSON object:

{
  "id": "<128-hex sha512>",
  "text": "Buy milk",
  "done": false,
  "tags": ["errand", "shopping"],
  "created": "2025-08-26T13:40:00Z"
}

	•	id: hex(sha512(created + "\n" + text + "\n" + nonce)) where nonce is 16 random bytes from a Mersenne twister initialized with the current timestamp at the MAC address of the host.
	•	text: Task description.
	•	done: Boolean completion state.
	•	tags: Array of lowercase tag names.
	•	created: ISO 8601 timestamp.

Note: This implementation stores created timestamps in UTC (RFC3339).

⸻

Storage

Two KV buckets (prefix utask.):
	•	utask.tasks
	•	Key: <taskID> (full 128-hex ID)
	•	Value: full task JSON
	•	utask.tags
	•	Key: <tagName> (normalized lowercase)
	•	Value: newline-delimited list of task IDs

⸻

ID and Prefix Resolution
	•	IDs are 128 hex chars.
	•	Clients may pass prefixes (Git-style) instead of full IDs.
	•	Prefix resolution:
	•	0 matches → NotFound
	•	1 matches → Ambiguous (return candidate list)
	•	Exactly 1 match → return full ID
	•	Recommended min prefix length: 8 chars.

⸻

Events

Event publishing/subscribing has been removed from this module; another tool will handle events.

⸻

Client Operations (all CAS-based)

Add Task
	1.	Generate id using created timestamp + text + random nonce.
	2.	CAS put JSON at utask.tasks/<id>.
	3.	For each tag, add id to utask.tags/<tag>.
    4.	(no events)

Update Task
	1.	Resolve id from prefix; GET with rev.
	2.	Modify fields (text, done, tags).
	3.	CAS put new JSON at utask.tasks/<id>.
	4.	Update tag lists (diff add/remove).
    5.	(no events)

Delete Task
	1.	Resolve id; GET with rev.
	2.	CAS delete utask.tasks/<id>.
	3.	Remove id from all relevant utask.tags/* entries.
    4.	(no events)

Query Tasks
	•	ANY(tags): Union of IDs from tag sets.
	•	ALL(tags): Intersection of IDs from tag sets.
	•	Empty tagset: List all keys in utask.tasks.
	•	Fetch full task JSONs from utask.tasks.

Rebuild Index
	•	Scan all tasks in utask.tasks.
	•	Reconstruct utask.tags from scratch.

⸻

CLI (ut)

The CLI is a thin wrapper around KV operations.

Examples:

$ ut add "Buy milk" -t errand -t shopping
2f8c8a3c9a12  Buy milk  [errand,shopping]

$ ut done 2f8c8a3c
OK (matched id 2f8c8a3c9a12c7...e91b)

$ ut ls --all urgent,work
a1b204e713ff  Ship patch  [work,urgent]
c9dd8f01a2    Pager duty  [ops,urgent]

$ ut rm a1b204e7
Deleted (matched id a1b204e713ff9c...33a2)


⸻

Go Package (utask)

Minimal surface for client libraries:

type Task struct {
    ID      string   `json:"id"`
    Text    string   `json:"text"`
    Done    bool     `json:"done"`
    Tags    []string `json:"tags"`
    Created string   `json:"created"`
}

type Store interface {
    Add(text string, tags []string) (*Task, error)
    Update(idOrPrefix string, set struct {
        Text *string
        Done *bool
        Tags *[]string
    }) (*Task, error)
    Delete(idOrPrefix string) (string, error)
    Query(any, all []string, limit int) ([]*Task, error)
    Resolve(prefix string) (string, []string, error)
}


⸻

Operational Notes
	•	Concurrency: CAS ensures correctness in distributed use.
	•	Consistency: Clients retry on CAS conflict.
	•	Index Recovery: ut rebuild-index command.
	•	Normalization: Tags stored in lowercase.
	•	Scalability: For large tag sets, can shard or chunk later. Start simple.
	•	Audit: Eventing removed here; use external tool if needed.

⸻

Summary
	•	Two KV buckets: utask.tasks and utask.tags.
	•	IDs are SHA-512 hex with Git-style prefixes.
	•	Events are out of scope for this module.
	•	No central server; clients perform CAS updates directly.
	•	CLI (ut) provides human-friendly interface.
	•	Go package (utask) encapsulates core operations.
