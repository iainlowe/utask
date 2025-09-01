package utask

import (
    "strings"
    "testing"
)

func TestDetailsExcludesTrailers(t *testing.T) {
    txt := "Title line\n\nBody line 1\nBody line 2\n\nCo-Authored-By: Jane <jane@example.com>\nReviewed-by: Bob <bob@example.com>\n"
    task := Task{Text: txt}

    details := task.Details()
    if strings.Contains(details, "Co-Authored-By") || strings.Contains(details, "Reviewed-by") {
        t.Fatalf("details should exclude trailers, got: %q", details)
    }
    want := "Body line 1\nBody line 2"
    if details != want {
        t.Fatalf("details mismatch\nwant:\n%q\ngot:\n%q", want, details)
    }

    trs := task.Trailers()
    if len(trs) != 2 {
        t.Fatalf("expected 2 trailers, got %d", len(trs))
    }
    if trs[0].Key != "Co-Authored-By" || !strings.Contains(trs[0].Value, "jane@example.com") {
        t.Fatalf("unexpected first trailer: %+v", trs[0])
    }
    if trs[1].Key != "Reviewed-by" || !strings.Contains(trs[1].Value, "bob@example.com") {
        t.Fatalf("unexpected second trailer: %+v", trs[1])
    }
}

