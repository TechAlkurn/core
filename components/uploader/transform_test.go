package uploader

import (
	"strings"

	"encoding/json"
	"github.com/TechAlkurn/core/lib"
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

// The S3 key _s3upload writes must equal the key in the URL Transform returns.
func TestS3ThumbKeyMatchesUpload(t *testing.T) {
	cmp := &uploader{AwsS3: true}
	fullPath, dir, name := cmp.FilePath("photo.jpg")
	if fullPath+name != "uploads/storage/"+dir+name {
		t.Fatalf("unexpected S3 key layout: %s", fullPath+name)
	}
	key, url := lib.ThumbS3(dir+name, 500, 600)
	if !strings.HasSuffix(url, "/"+key) || !strings.HasPrefix(key, "uploads/cache/") || !strings.HasSuffix(key, "-500x600.jpg") {
		t.Fatalf("key %q url %q", key, url)
	}
}
