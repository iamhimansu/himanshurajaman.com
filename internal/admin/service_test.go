package admin_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/iamhimansu/himanshurajaman.com/internal/admin"
	"github.com/iamhimansu/himanshurajaman.com/internal/content"
	"github.com/iamhimansu/himanshurajaman.com/internal/github"
	"github.com/iamhimansu/himanshurajaman.com/internal/ingest"
	"github.com/iamhimansu/himanshurajaman.com/internal/source"
	"github.com/iamhimansu/himanshurajaman.com/internal/store"
	"github.com/iamhimansu/himanshurajaman.com/internal/testutil"
)

const seededSHA = "1111111111111111111111111111111111111111"

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cache.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// newService builds a Service wired to the given mock GitHub server, returning
// the service and its backing store.
func newService(t *testing.T, m *testutil.GitHubMock) (*admin.Service, *store.Store) {
	t.Helper()
	gh := github.NewClient("tok", github.WithAPIBase(m.APIBase()))
	return newServiceWithClient(t, gh, m.Owner, m.Repo)
}

// newServiceWithClient builds a Service around an arbitrary GitHub client (used
// for the transport-error variant), returning the service and its store.
func newServiceWithClient(t *testing.T, gh *github.Client, owner, repo string) (*admin.Service, *store.Store) {
	t.Helper()
	st := openStore(t)
	imgs, err := ingest.NewImageManager(t.TempDir())
	if err != nil {
		t.Fatalf("image manager: %v", err)
	}
	ing := ingest.New(source.NewLocal(t.TempDir()), st, content.NewRenderer(), imgs, discardLogger())
	svc := admin.New(st, gh, content.NewRenderer(), ing, imgs,
		admin.Config{RepoOwner: owner, RepoName: repo, ContentRef: "main"}, discardLogger())
	return svc, st
}

func postBytes(slug, title string) []byte {
	return testutil.PostJSON(testutil.PostInput{
		Slug: slug, Title: title, Summary: "s", Date: "2024-01-01T00:00:00Z",
		Published: true, Body: title + " body",
	})
}

func parsePost(t *testing.T, slug, title string) (*content.ParsedPost, []byte) {
	t.Helper()
	data := postBytes(slug, title)
	p, err := content.ParsePost("posts/"+slug+".json", data)
	if err != nil {
		t.Fatalf("parse post: %v", err)
	}
	return p, data
}

func seedPost(t *testing.T, st *store.Store, slug, title, fileSHA string) {
	t.Helper()
	err := st.UpsertPost(context.Background(), content.Post{
		Slug: slug, Title: title, Summary: "s", Document: `{"blocks":[]}`, HTML: "<p>x</p>",
		PlainText: "x", Published: true, PublishedAt: "2024-01-01T00:00:00Z",
		ReadingMinutes: 1, FilePath: "posts/" + slug + ".json", FileSHA: fileSHA,
	})
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
}

func TestUpdatePost_StaleShaReturnsStaleErrorAndReconciles(t *testing.T) {
	for _, status := range []int{409, 422} {
		t.Run(httpName(status), func(t *testing.T) {
			ctx := context.Background()
			m := testutil.NewGitHubMock(t)
			svc, st := newService(t, m)
			seedPost(t, st, "p1", "Old Cached", seededSHA)

			const liveSHA = "2222222222222222222222222222222222222222"
			liveData := postBytes("p1", "Reconciled Live")
			m.OnPutContents("posts/p1.json", testutil.GHStatus(status))
			m.OnGetContents("posts/p1.json", testutil.GHContents(liveData, liveSHA))

			attempted, data := parsePost(t, "p1", "Attempted")
			_, err := svc.UpdatePost(ctx, attempted, data, "stale-client-sha")

			var se *admin.StaleError
			if !errors.As(err, &se) {
				t.Fatalf("UpdatePost error = %v, want *StaleError", err)
			}
			if se.CurrentFileSHA != liveSHA {
				t.Errorf("StaleError.CurrentFileSHA = %q, want %q", se.CurrentFileSHA, liveSHA)
			}

			ap, err := st.GetAdminPost(ctx, "p1")
			if err != nil {
				t.Fatalf("GetAdminPost: %v", err)
			}
			if ap.Title != "Reconciled Live" {
				t.Errorf("cache not reconciled to live content: title=%q", ap.Title)
			}
			if ap.Title == "Attempted" {
				t.Error("the attempted content must NOT have been written to the cache")
			}
			if ap.FileSHA != liveSHA {
				t.Errorf("cache sha not reconciled: %q, want %q", ap.FileSHA, liveSHA)
			}
		})
	}
}

func TestDeletePost_StaleSha(t *testing.T) {
	ctx := context.Background()
	m := testutil.NewGitHubMock(t)
	svc, st := newService(t, m)
	seedPost(t, st, "p1", "Old Cached", seededSHA)

	const liveSHA = "3333333333333333333333333333333333333333"
	m.OnDeleteContents("posts/p1.json", testutil.GHStatus(409))
	m.OnGetContents("posts/p1.json", testutil.GHContents(postBytes("p1", "Reconciled Live"), liveSHA))

	err := svc.DeletePost(ctx, "p1", "stale-client-sha")
	var se *admin.StaleError
	if !errors.As(err, &se) {
		t.Fatalf("DeletePost error = %v, want *StaleError", err)
	}
	if se.CurrentFileSHA != liveSHA {
		t.Errorf("StaleError.CurrentFileSHA = %q, want %q", se.CurrentFileSHA, liveSHA)
	}
	// A stale delete reconciles rather than deleting: the row survives.
	if ap, err := st.GetAdminPost(ctx, "p1"); err != nil || ap.Title != "Reconciled Live" {
		t.Errorf("expected reconciled row to survive stale delete, got %+v err=%v", ap, err)
	}
}

func TestDeletePost_ConflictLiveNotFoundRemovesRow(t *testing.T) {
	ctx := context.Background()
	m := testutil.NewGitHubMock(t)
	svc, st := newService(t, m)
	seedPost(t, st, "p1", "Old Cached", seededSHA)

	// Conflict, then the live GET 404s (file deleted out-of-band): drop the row.
	m.OnDeleteContents("posts/p1.json", testutil.GHStatus(409))
	m.OnGetContents("posts/p1.json", testutil.GHStatus(404))

	err := svc.DeletePost(ctx, "p1", "stale-client-sha")
	var se *admin.StaleError
	if !errors.As(err, &se) {
		t.Fatalf("DeletePost error = %v, want *StaleError", err)
	}
	if se.CurrentFileSHA != "" {
		t.Errorf("CurrentFileSHA should be empty when the file is gone, got %q", se.CurrentFileSHA)
	}
	if _, err := st.GetAdminPost(ctx, "p1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cache row should be removed, got %v", err)
	}
}

func TestUpdatePost_GitHubUnavailable_CacheUnchanged(t *testing.T) {
	t.Run("5xx", func(t *testing.T) {
		ctx := context.Background()
		m := testutil.NewGitHubMock(t)
		svc, st := newService(t, m)
		seedPost(t, st, "p1", "Old Cached", seededSHA)
		m.OnPutContents("posts/p1.json", testutil.GHStatus(500))

		attempted, data := parsePost(t, "p1", "Attempted")
		_, err := svc.UpdatePost(ctx, attempted, data, seededSHA)
		if !errors.Is(err, admin.ErrGitHubWrite) {
			t.Fatalf("err = %v, want ErrGitHubWrite", err)
		}
		assertUnchanged(t, st, "p1", "Old Cached", seededSHA)
	})

	t.Run("transport error", func(t *testing.T) {
		ctx := context.Background()
		gh := github.NewClient("tok",
			github.WithAPIBase("http://unused.invalid"),
			github.WithHTTPClient(testutil.FailingHTTPClient()))
		svc, st := newServiceWithClient(t, gh, "octocat", "content")
		seedPost(t, st, "p1", "Old Cached", seededSHA)

		attempted, data := parsePost(t, "p1", "Attempted")
		_, err := svc.UpdatePost(ctx, attempted, data, seededSHA)
		if !errors.Is(err, admin.ErrGitHubWrite) {
			t.Fatalf("err = %v, want ErrGitHubWrite", err)
		}
		assertUnchanged(t, st, "p1", "Old Cached", seededSHA)
	})
}

func TestCreatePost_GitHubUnavailable_CacheUnchanged(t *testing.T) {
	ctx := context.Background()
	m := testutil.NewGitHubMock(t)
	svc, st := newService(t, m)
	m.OnPutContents("posts/new.json", testutil.GHStatus(500))

	created, data := parsePost(t, "new", "New Post")
	if _, err := svc.CreatePost(ctx, created, data); !errors.Is(err, admin.ErrGitHubWrite) {
		t.Fatalf("err = %v, want ErrGitHubWrite", err)
	}
	if exists, _ := st.PostExists(ctx, "new"); exists {
		t.Error("GitHub-first invariant violated: cache gained a post after a failed write")
	}
}

func TestCreatePost_CollisionErrPostExists(t *testing.T) {
	t.Run("slug already cached makes no github call", func(t *testing.T) {
		ctx := context.Background()
		m := testutil.NewGitHubMock(t)
		svc, st := newService(t, m)
		seedPost(t, st, "p1", "Existing", seededSHA)

		created, data := parsePost(t, "p1", "Dup Attempt")
		if _, err := svc.CreatePost(ctx, created, data); !errors.Is(err, admin.ErrPostExists) {
			t.Fatalf("err = %v, want ErrPostExists", err)
		}
		if m.RequestCount() != 0 {
			t.Errorf("expected NO GitHub call for a cache-known collision, got %d", m.RequestCount())
		}
	})

	t.Run("empty cache but github conflict", func(t *testing.T) {
		ctx := context.Background()
		m := testutil.NewGitHubMock(t)
		svc, _ := newService(t, m)
		m.OnPutContents("posts/p2.json", testutil.GHStatus(409))

		created, data := parsePost(t, "p2", "Collides On GitHub")
		if _, err := svc.CreatePost(ctx, created, data); !errors.Is(err, admin.ErrPostExists) {
			t.Fatalf("err = %v, want ErrPostExists", err)
		}
	})
}

func TestCreatePost_Success(t *testing.T) {
	ctx := context.Background()
	m := testutil.NewGitHubMock(t)
	svc, st := newService(t, m)
	const newSHA = "4444444444444444444444444444444444444444"
	m.OnPutContents("posts/p3.json", testutil.GHWrite(201, newSHA, "commit-sha"))

	created, data := parsePost(t, "p3", "Brand New")
	gotSHA, err := svc.CreatePost(ctx, created, data)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if gotSHA != newSHA {
		t.Errorf("returned sha = %q, want %q", gotSHA, newSHA)
	}
	ap, err := st.GetAdminPost(ctx, "p3")
	if err != nil {
		t.Fatalf("GetAdminPost: %v", err)
	}
	if ap.Title != "Brand New" || ap.FileSHA != newSHA {
		t.Errorf("cache not updated after create: title=%q sha=%q", ap.Title, ap.FileSHA)
	}
}

func assertUnchanged(t *testing.T, st *store.Store, slug, wantTitle, wantSHA string) {
	t.Helper()
	ap, err := st.GetAdminPost(context.Background(), slug)
	if err != nil {
		t.Fatalf("GetAdminPost: %v", err)
	}
	if ap.Title != wantTitle || ap.FileSHA != wantSHA {
		t.Errorf("cache changed after failed GitHub write: title=%q sha=%q, want %q/%q",
			ap.Title, ap.FileSHA, wantTitle, wantSHA)
	}
}

func httpName(status int) string {
	switch status {
	case 409:
		return "conflict-409"
	case 422:
		return "unprocessable-422"
	default:
		return "status"
	}
}
