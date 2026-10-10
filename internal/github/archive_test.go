package github_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/github"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

func TestExtractTarGz_StripsTopLevelPrefix(t *testing.T) {
	gz := testutil.MakeTarGz("octocat-content-abc1234", map[string][]byte{
		"posts/hello.json":   []byte(`{"meta":{}}`),
		"resume/resume.json": []byte(`{"basics":{}}`),
	})

	files, err := github.ExtractTarGz(bytes.NewReader(gz))
	if err != nil {
		t.Fatalf("ExtractTarGz: %v", err)
	}

	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = string(f.Data)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 regular files, got %d: %v", len(got), got)
	}
	if got["posts/hello.json"] != `{"meta":{}}` {
		t.Errorf("post content wrong or path not stripped: %v", got)
	}
	if _, ok := got["resume/resume.json"]; !ok {
		t.Errorf("resume path not stripped: %v", got)
	}
}

func TestExtractTarGz_SkipsNonRegularEntries(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	const top = "octocat-content-deadbee"
	// A directory entry, a symlink, and one regular file.
	_ = tw.WriteHeader(&tar.Header{Name: top + "/", Typeflag: tar.TypeDir, Mode: 0o755})
	_ = tw.WriteHeader(&tar.Header{Name: top + "/posts", Typeflag: tar.TypeDir, Mode: 0o755})
	_ = tw.WriteHeader(&tar.Header{Name: top + "/link", Typeflag: tar.TypeSymlink, Linkname: "posts/real.json"})
	data := []byte(`{"ok":true}`)
	_ = tw.WriteHeader(&tar.Header{Name: top + "/posts/real.json", Typeflag: tar.TypeReg, Size: int64(len(data)), Mode: 0o644})
	_, _ = tw.Write(data)
	_ = tw.Close()
	_ = gz.Close()

	files, err := github.ExtractTarGz(&buf)
	if err != nil {
		t.Fatalf("ExtractTarGz: %v", err)
	}
	if len(files) != 1 || files[0].Path != "posts/real.json" {
		t.Fatalf("expected only the regular file posts/real.json, got %+v", files)
	}
}

func TestExtractTarGz_RejectsNotGzip(t *testing.T) {
	if _, err := github.ExtractTarGz(bytes.NewReader([]byte("not a gzip stream"))); err == nil {
		t.Fatal("expected error for non-gzip input")
	}
}
