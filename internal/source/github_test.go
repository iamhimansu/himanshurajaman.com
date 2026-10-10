package source_test

import (
	"context"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/github"
	"github.com/iamhimansu/himanshurajaman.com/internal/source"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

func TestGitHubSource_Fetch(t *testing.T) {
	const sha = "abcdef0123456789abcdef0123456789abcdef01"
	m := testutil.NewGitHubMock(t)
	m.OnCommitSHA("main", sha)
	m.OnTarball("main", testutil.MakeTarGz("octocat-content-abcdef0", map[string][]byte{
		"posts/hello.json":   []byte(`{"meta":{"slug":"hello"}}`),
		"resume/resume.json": []byte(`{"basics":{}}`),
		"settings/site.json": []byte(`{"title":"x"}`),
	}))

	client := github.NewClient("tok", github.WithAPIBase(m.APIBase()))
	src := source.NewGitHub(client, m.Owner, m.Repo, "main")

	snap, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.CommitSHA != sha {
		t.Errorf("CommitSHA = %q, want full sha %q", snap.CommitSHA, sha)
	}
	if snap.Ref != "main" {
		t.Errorf("Ref = %q, want main", snap.Ref)
	}

	paths := map[string]bool{}
	for _, f := range snap.Files {
		paths[f.Path] = true
	}
	for _, want := range []string{"posts/hello.json", "resume/resume.json", "settings/site.json"} {
		if !paths[want] {
			t.Errorf("snapshot missing stripped path %q; got %v", want, paths)
		}
	}
}
