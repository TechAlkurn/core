package uploader

import (
	"encoding/json"
	"testing"
)

func TestTransformMap(t *testing.T) {
	t.Setenv("STORAGE_PATH", "https://uploads.example.test/storage")
	for _, in := range []map[string]any{
		{"path": "a/b/x.mp4", "mimetype": "video/mp4"},
		{"path": []byte("a/b/x.mp4"), "mimetype": []byte("video/mp4")},
	} {
		out := transformMap(in)
		b, _ := json.Marshal(out)
		if out["path"] != "https://uploads.example.test/storage/a/b/x.mp4" || out["video_url"] != out["path"] || out["type"] != "video" || out["full_mimetype"] != "video/mp4" {
			t.Fatalf("unexpected: %s", b)
		}
		if mt, _ := out["mimetype"].(*string); mt == nil || *mt != "video" {
			t.Fatalf("mimetype: %s", b)
		}
	}
}
