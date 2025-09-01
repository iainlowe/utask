package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	conf "github.com/iainlowe/utask/internal/config"
	"github.com/iainlowe/utask/internal/utask"
	cli "github.com/urfave/cli/v2"
)

// appMetaKey is used to stash config into cli.App metadata
const appMetaKey = "config"

func main() {
	app := &cli.App{
		Name:  "ut",
		Usage: "Minimal task queue CLI and MCP server",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "path to config file", EnvVars: []string{"UTASK_CONFIG"}},
			&cli.StringFlag{Name: "nats-url", Usage: "NATS server URL", EnvVars: []string{"UTASK_NATS_URL"}},
			&cli.StringFlag{Name: "openai-api-key", Usage: "OpenAI API key", EnvVars: []string{"OPENAI_API_KEY"}},
			&cli.StringFlag{Name: "openai-model", Usage: "OpenAI model name", EnvVars: []string{"UTASK_OPENAI_MODEL"}},
			&cli.StringFlag{Name: "profile", Usage: "profile/namespace", EnvVars: []string{"UTASK_PROFILE"}},
			&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "increase verbosity"},
		},
		Before: func(c *cli.Context) error {
			// Determine config file path
			cfgPath := c.String("config")
			if cfgPath == "" {
				if env := os.Getenv("UTASK_CONFIG"); env != "" {
					cfgPath = env
				} else {
					def, err := conf.DefaultPath()
					if err != nil {
						return err
					}
					cfgPath = def
				}
			}

			// Load config from file (lowest precedence)
			cfg, err := conf.LoadFromFile(cfgPath)
			if err != nil {
				return err
			}

			// Overlay env
			conf.OverlayEnv(cfg)

			// Overlay flags (highest precedence)
			if c.IsSet("nats-url") {
				cfg.NATS.URL = c.String("nats-url")
			}
			if c.IsSet("openai-api-key") {
				cfg.OpenAI.APIKey = c.String("openai-api-key")
			}
			if c.IsSet("openai-model") {
				cfg.OpenAI.Model = c.String("openai-model")
			}
			if c.IsSet("profile") {
				cfg.UI.Profile = c.String("profile")
			}

			// Defaults if still empty
			if cfg.NATS.URL == "" {
				cfg.NATS.URL = "neo:4222"
			}
			if cfg.UI.Profile == "" {
				cfg.UI.Profile = "default"
			}

			// Stash in metadata for commands
			if c.App.Metadata == nil {
				c.App.Metadata = map[string]interface{}{}
			}
			c.App.Metadata[appMetaKey] = cfg
			return nil
		},
		Commands: []*cli.Command{
			{
				Name:  "mcp",
				Usage: "Run MCP server",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "stdio", Usage: "run MCP over stdio"},
				},
				Action: func(c *cli.Context) error {
					if c.Bool("stdio") {
						return runMCPStdio(c)
					}
					return cli.ShowSubcommandHelp(c)
				},
			},
			{Name: "create", Usage: "Create a task", Flags: []cli.Flag{
				&cli.StringFlag{Name: "title", Usage: "task text/title"},
				&cli.StringSliceFlag{Name: "tag", Usage: "task tag (repeatable)"},
				// Single text field; no separate extended/description
				&cli.IntFlag{Name: "priority", Value: 1, Usage: "priority (1=highest)"},
				&cli.IntFlag{Name: "estimate-min", Usage: "estimate in minutes"},
			}, Action: cmdCreate},
			{Name: "list", Usage: "List tasks", Flags: []cli.Flag{
				&cli.StringFlag{Name: "tag", Usage: "filter by single tag"},
				&cli.StringFlag{Name: "tags", Usage: "ANY match: comma-separated tags"},
				&cli.StringFlag{Name: "all-tags", Usage: "ALL match: comma-separated tags"},
				&cli.StringFlag{Name: "status", Usage: "filter by status: open|closed"},
			}, Action: cmdList},
			{Name: "get", Usage: "Get a task", Action: cmdGet},
			{Name: "close", Usage: "Close a task", Action: cmdClose},
			{Name: "reopen", Usage: "Reopen a task", Action: cmdReopen},
			{Name: "update", Usage: "Update a task text/tags", Flags: []cli.Flag{
				&cli.StringFlag{Name: "text", Usage: "new task text"},
				&cli.StringFlag{Name: "title", Usage: "new title/text"},
				// Single text field; no separate extended/description
				&cli.StringSliceFlag{Name: "tag", Usage: "replace tags (repeatable)"},
				&cli.StringFlag{Name: "tags", Usage: "replace tags (comma-separated)"},
				&cli.BoolFlag{Name: "done", Usage: "set done true/false"},
				&cli.IntFlag{Name: "priority", Usage: "update priority"},
			}, Action: cmdUpdate},
			{Name: "delete", Usage: "Delete a task", Aliases: []string{"rm"}, Action: cmdDelete},
			{Name: "tags", Usage: "List tags", Action: cmdTags},
            {Name: "rebuild-index", Usage: "Rebuild tag index", Action: cmdRebuildIndex},
            {Name: "check", Usage: "Check tasks for trailer issues", Flags: []cli.Flag{
                &cli.StringFlag{Name: "tag", Usage: "filter by tag"},
                &cli.StringFlag{Name: "status", Usage: "filter by status: open|closed"},
            }, Action: cmdCheck},
        },
    }

	if err := app.Run(os.Args); err != nil {
		// Print to stderr and exit non-zero
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func getConfig(c *cli.Context) *conf.Config {
	if c.App == nil || c.App.Metadata == nil {
		return &conf.Config{}
	}
	if v, ok := c.App.Metadata[appMetaKey].(*conf.Config); ok {
		return v
	}
	return &conf.Config{}
}

// --- Command stubs ---

func cmdCreate(c *cli.Context) error {
	cfg := getConfig(c)
	if strings.TrimSpace(c.String("title")) == "" {
		return fmt.Errorf("--title is required")
	}
	ctx := context.Background()
	store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
	if err != nil {
		return err
	}
	defer store.Close()
	in := utask.TaskInput{
		Text:            c.String("title"),
		Tags:            c.StringSlice("tag"),
		Priority:        c.Int("priority"),
		EstimateMinutes: c.Int("estimate-min"),
	}
	t, existed, err := store.CreateTask(ctx, in)
	if err != nil {
		return err
	}
	if c.Bool("verbose") {
		b, _ := json.MarshalIndent(t, "", "  ")
		fmt.Println(string(b))
	} else {
		if existed {
			fmt.Println(t.ID, "(exists)")
		} else {
			fmt.Println(t.ID)
		}
	}
	return nil
}

func cmdList(c *cli.Context) error {
	cfg := getConfig(c)
	ctx := context.Background()
	store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
	if err != nil {
		return err
	}
	defer store.Close()
	var sf utask.Status
	if s := c.String("status"); s != "" {
		switch s {
		case string(utask.StatusOpen):
			sf = utask.StatusOpen
		case string(utask.StatusClosed):
			sf = utask.StatusClosed
		default:
			return fmt.Errorf("invalid --status: %s", s)
		}
	}
	var tasks []utask.Task
	anyTags := parseCSVTags(c.String("tags"))
	allTags := parseCSVTags(c.String("all-tags"))
	if len(anyTags) > 0 || len(allTags) > 0 {
		tasks, err = store.Query(ctx, anyTags, allTags, 0)
		if err != nil {
			return err
		}
		if sf != "" {
			filtered := make([]utask.Task, 0, len(tasks))
			for _, t := range tasks {
				if sf == utask.StatusOpen && t.Done {
					continue
				}
				if sf == utask.StatusClosed && !t.Done {
					continue
				}
				filtered = append(filtered, t)
			}
			tasks = filtered
		}
	} else {
		tasks, err = store.List(ctx, c.String("tag"), sf)
		if err != nil {
			return err
		}
	}
	if c.Bool("verbose") {
		b, _ := json.MarshalIndent(tasks, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	for _, t := range tasks {
		st := "open"
		if t.Done {
			st = "closed"
		}
		created := t.Created
		fmt.Printf("%s\t%s\t%s\t[%s]\n", t.ID, st, created, strings.Join(t.Tags, ","))
		fmt.Println("  ", t.Text)
	}
	return nil
}

func parseCSVTags(in string) []string {
	if strings.TrimSpace(in) == "" {
		return nil
	}
	parts := strings.Split(in, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func cmdGet(c *cli.Context) error {
	if c.NArg() < 1 {
		return fmt.Errorf("usage: ut get <id>")
	}
	id := c.Args().First()
	cfg := getConfig(c)
	ctx := context.Background()
	store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
	if err != nil {
		return err
	}
	defer store.Close()
	rid, candidates, err := store.Resolve(id)
	if err != nil {
		if len(candidates) > 1 {
			return fmt.Errorf("ambiguous prefix; candidates: %s", strings.Join(candidates, ", "))
		}
		return err
	}
	t, _, err := store.GetTask(ctx, rid)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(t, "", "  ")
	fmt.Println(string(b))
	return nil
}

func cmdClose(c *cli.Context) error {
	if c.NArg() < 1 {
		return fmt.Errorf("usage: ut close <id>")
	}
	id := c.Args().First()
	cfg := getConfig(c)
	ctx := context.Background()
	store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
	if err != nil {
		return err
	}
	defer store.Close()
	rid, candidates, err := store.Resolve(id)
	if err != nil {
		if len(candidates) > 1 {
			return fmt.Errorf("ambiguous prefix; candidates: %s", strings.Join(candidates, ", "))
		}
		return err
	}
	t, changed, err := store.CloseTask(ctx, rid)
	if err != nil {
		return err
	}
	if c.Bool("verbose") {
		b, _ := json.MarshalIndent(t, "", "  ")
		fmt.Println(string(b))
	} else {
		if changed {
			fmt.Println(t.ID, "closed")
		} else {
			fmt.Println(t.ID, "already closed")
		}
	}
	return nil
}

func cmdReopen(c *cli.Context) error {
	if c.NArg() < 1 {
		return fmt.Errorf("usage: ut reopen <id>")
	}
	id := c.Args().First()
	cfg := getConfig(c)
	ctx := context.Background()
	store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
	if err != nil {
		return err
	}
	defer store.Close()
	rid, candidates, err := store.Resolve(id)
	if err != nil {
		if len(candidates) > 1 {
			return fmt.Errorf("ambiguous prefix; candidates: %s", strings.Join(candidates, ", "))
		}
		return err
	}
	t, changed, err := store.ReopenTask(ctx, rid)
	if err != nil {
		return err
	}
	if c.Bool("verbose") {
		b, _ := json.MarshalIndent(t, "", "  ")
		fmt.Println(string(b))
	} else {
		if changed {
			fmt.Println(t.ID, "reopened")
		} else {
			fmt.Println(t.ID, "already open")
		}
	}
	return nil
}

// events command removed

func cmdTags(c *cli.Context) error {
	cfg := getConfig(c)
	ctx := context.Background()
	store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
	if err != nil {
		return err
	}
	defer store.Close()
	counts, err := store.ListTags()
	if err != nil {
		return err
	}
	for k, v := range counts {
		fmt.Printf("%s\t%d\n", k, v)
	}
	return nil
}

func cmdRebuildIndex(c *cli.Context) error {
	cfg := getConfig(c)
	ctx := context.Background()
	store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.RebuildIndex(ctx); err != nil {
		return err
	}
	fmt.Println("OK")
	return nil
}

func cmdCheck(c *cli.Context) error {
    cfg := getConfig(c)
    ctx := context.Background()
    store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
    if err != nil { return err }
    defer store.Close()
    var sf utask.Status
    if s := c.String("status"); s != "" {
        switch s {
        case string(utask.StatusOpen): sf = utask.StatusOpen
        case string(utask.StatusClosed): sf = utask.StatusClosed
        default: return fmt.Errorf("invalid --status: %s", s)
        }
    }
    tasks, err := store.List(ctx, c.String("tag"), sf)
    if err != nil { return err }
    issues := 0
    for _, t := range tasks {
        drops := t.TrailerDrops()
        if len(drops) == 0 {
            continue
        }
        issues++
        fmt.Printf("%s\t%s\n", t.ID, t.Short())
        fmt.Println("  Dropped lines from trailer block:")
        for _, line := range drops {
            fmt.Println("   -", line)
        }
    }
    if issues == 0 {
        fmt.Println("OK")
    }
    return nil
}

func cmdUpdate(c *cli.Context) error {
	if c.NArg() < 1 {
		return fmt.Errorf("usage: ut update <id> [--title s] [--tag t ...] [--tags a,b]")
	}
	id := c.Args().First()
	cfg := getConfig(c)
	ctx := context.Background()
	store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
	if err != nil {
		return err
	}
	defer store.Close()

	rid, cands, err := store.Resolve(id)
	if err != nil {
		if len(cands) > 1 {
			return fmt.Errorf("ambiguous prefix; candidates: %s", strings.Join(cands, ", "))
		}
		return err
	}

	var set utask.UpdateSet
	// Prefer --text; fallback to --title for compatibility
	if s := strings.TrimSpace(c.String("text")); s != "" {
		set.Text = &s
	} else if s := strings.TrimSpace(c.String("title")); s != "" {
		set.Text = &s
	}
	// No separate extended/description field; only text, tags, done, priority
	if c.IsSet("priority") {
		p := c.Int("priority")
		set.Priority = &p
	}
	if c.IsSet("done") {
		b := c.Bool("done")
		set.Done = &b
	}
	tags := []string{}
	tags = append(tags, parseCSVTags(c.String("tags"))...)
	tags = append(tags, c.StringSlice("tag")...)
	if len(tags) > 0 {
		// normalize via parseCSVTags logic for repeatable flags
		joined := strings.Join(tags, ",")
		n := parseCSVTags(joined)
		set.Tags = &n
	}

	t, err := store.UpdateTask(ctx, rid, set)
	if err != nil {
		return err
	}
	if c.Bool("verbose") {
		b, _ := json.MarshalIndent(t, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println(t.ID, "updated")
	}
	return nil
}

func cmdDelete(c *cli.Context) error {
	if c.NArg() < 1 {
		return fmt.Errorf("usage: ut delete <id>")
	}
	id := c.Args().First()
	cfg := getConfig(c)
	ctx := context.Background()
	store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
	if err != nil {
		return err
	}
	defer store.Close()
	rid, cands, err := store.Resolve(id)
	if err != nil {
		if len(cands) > 1 {
			return fmt.Errorf("ambiguous prefix; candidates: %s", strings.Join(cands, ", "))
		}
		return err
	}
	delID, err := store.DeleteTask(ctx, rid)
	if err != nil {
		return err
	}
	fmt.Println(delID, "deleted")
	return nil
}

func runMCPStdio(c *cli.Context) error {
	// Basic MCP-style JSON-RPC loop with tools/list and tools/call
	log.SetOutput(os.Stderr)
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	type msg struct {
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		JSONRPC string          `json:"jsonrpc"`
		Params  json.RawMessage `json:"params"`
	}
	type resp struct {
		ID      any         `json:"id"`
		JSONRPC string      `json:"jsonrpc"`
		Result  interface{} `json:"result,omitempty"`
		Error   interface{} `json:"error,omitempty"`
	}
	tools := []string{"create", "list", "get", "close", "reopen"}

	cfg := getConfig(c)
	ctx := context.Background()
	store, err := utask.Open(ctx, cfg.NATS.URL, cfg.UI.Profile)
	if err != nil {
		return err
	}
	defer store.Close()

	for {
		var m msg
		if err := dec.Decode(&m); err != nil {
			return nil // graceful exit on EOF
		}
		r := resp{ID: m.ID, JSONRPC: "2.0"}
		switch m.Method {
		case "initialize":
			r.Result = map[string]any{"capabilities": map[string]any{"tools": tools}}
		case "tools/list":
			r.Result = map[string]any{"tools": tools}
		case "tools/call":
			var p struct {
				Name string                 `json:"name"`
				Args map[string]interface{} `json:"arguments"`
			}
			if err := json.Unmarshal(m.Params, &p); err != nil {
				r.Error = err.Error()
				break
			}
			switch p.Name {
			case "create":
				title, _ := p.Args["title"].(string)
				var tags []string
				if v, ok := p.Args["tags"].([]interface{}); ok {
					for _, it := range v {
						if s, ok := it.(string); ok {
							tags = append(tags, s)
						}
					}
				}
				in := utask.TaskInput{Text: title, Tags: tags}
				t, _, err := store.CreateTask(ctx, in)
				if err != nil {
					r.Error = err.Error()
					break
				}
				r.Result = t
			case "list":
				tag, _ := p.Args["tag"].(string)
				var sf utask.Status
				if s, ok := p.Args["status"].(string); ok {
					switch s {
					case string(utask.StatusOpen):
						sf = utask.StatusOpen
					case string(utask.StatusClosed):
						sf = utask.StatusClosed
					}
				}
				ts, err := store.List(ctx, tag, sf)
				if err != nil {
					r.Error = err.Error()
					break
				}
				r.Result = ts
			case "get":
				id, _ := p.Args["id"].(string)
				rid, _, err := store.Resolve(id)
				if err != nil {
					r.Error = err.Error()
					break
				}
				t, _, err := store.GetTask(ctx, rid)
				if err != nil {
					r.Error = err.Error()
					break
				}
				r.Result = t
			case "close":
				id, _ := p.Args["id"].(string)
				rid, _, err := store.Resolve(id)
				if err != nil {
					r.Error = err.Error()
					break
				}
				t, _, err := store.CloseTask(ctx, rid)
				if err != nil {
					r.Error = err.Error()
					break
				}
				r.Result = t
			case "reopen":
				id, _ := p.Args["id"].(string)
				rid, _, err := store.Resolve(id)
				if err != nil {
					r.Error = err.Error()
					break
				}
				t, _, err := store.ReopenTask(ctx, rid)
				if err != nil {
					r.Error = err.Error()
					break
				}
				r.Result = t
			default:
				r.Error = fmt.Sprintf("unknown tool: %s", p.Name)
			}
		default:
			r.Error = fmt.Sprintf("unknown method: %s", m.Method)
		}
		if err := enc.Encode(&r); err != nil {
			return nil
		}
	}
}
