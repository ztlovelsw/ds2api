package devcapture

import (
	"io"
	"strings"
	"testing"
)

func TestStorePushKeepsNewestWithinLimit(t *testing.T) {
	s := &Store{enabled: true, limit: 2, maxBodyBytes: 1024}
	for i := 0; i < 3; i++ {
		session := s.Start("test", "http://x", "", map[string]any{"seq": i})
		if session == nil {
			t.Fatal("expected session")
		}
		rc := session.WrapBody(io.NopCloser(strings.NewReader("ok")), 200)
		_, _ = io.ReadAll(rc)
		_ = rc.Close()
	}
	items := s.Snapshot()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !strings.Contains(items[0].RequestBody, `"seq":2`) {
		t.Fatalf("expected newest first, got %#v", items[0].RequestBody)
	}
	if !strings.Contains(items[1].RequestBody, `"seq":1`) {
		t.Fatalf("expected second newest, got %#v", items[1].RequestBody)
	}
}

func TestWrapBodyTruncatesByLimit(t *testing.T) {
	s := &Store{enabled: true, limit: 5, maxBodyBytes: 4}
	session := s.Start("test", "http://x", "acc1", map[string]any{"x": 1})
	if session == nil {
		t.Fatal("expected session")
	}
	rc := session.WrapBody(io.NopCloser(strings.NewReader("abcdef")), 200)
	_, _ = io.ReadAll(rc)
	_ = rc.Close()

	items := s.Snapshot()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].ResponseBody != "abcd" {
		t.Fatalf("expected truncated body, got %q", items[0].ResponseBody)
	}
	if !items[0].ResponseTruncated {
		t.Fatal("expected truncated flag true")
	}
	if items[0].AccountID != "acc1" {
		t.Fatalf("expected account id, got %q", items[0].AccountID)
	}
}
