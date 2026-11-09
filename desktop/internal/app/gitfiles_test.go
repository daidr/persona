package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
	"github.com/nwverse/persona/desktop/internal/hubs"
)

// weatherAgent is the mock's agent working in its sample checkout.
func weatherAgent(t *testing.T, a *App) hubapi.Session {
	t.Helper()
	for _, list := range a.conn.data.Sessions {
		for _, s := range list {
			if s.Cwd == "/Users/me/code/weather" {
				return s
			}
		}
	}
	t.Fatal("no agent in the weather folder")
	return hubapi.Session{}
}

func TestGitWrite(t *testing.T) {
	a, _ := signedInTo(t, gitMockURL, "en")
	// the socket's hello says what the hub has; the tests leave the socket out
	if a.gitWritable() {
		t.Fatal("git_write before the hello")
	}
	a.conn.data.Hello.Features = []string{"git_write"}
	x := weatherAgent(t, a)
	key := x.Key()
	a.Navigate("/agents/" + key)
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	a.chat(key).pane = paneChanges
	tt.Frame()
	tt.Frame()
	st := a.changes(key)
	if !st.loaded || st.status.Branch != "widget-hourly" || len(st.status.Files) != 4 {
		t.Fatalf("status: %+v", st.status)
	}
	if !tt.HasText(a.tr("gitBranches")) || !tt.HasText(a.tr("gitCommit")) {
		t.Fatalf("writes: %v", tt.Texts())
	}
	shot(t, tt, "git-light")
	tt.SetDark(true)
	shot(t, tt, "git-dark")
	tt.SetDark(false)

	// stage the unstaged file, then commit what is staged
	x0 := hubapi.GitFile{}
	for _, f := range st.status.Files {
		if f.Path == "WeatherWidget/Provider.swift" {
			x0 = f
		}
	}
	if stageState(x0) != ui.CheckOff {
		t.Fatalf("Provider.swift: %+v", x0)
	}
	a.gitRun(key, x, "stage:"+x0.Path, "stage", map[string]any{"paths": []string{x0.Path}, "unstage": false}, "", nil)
	for _, f := range st.status.Files {
		if f.Path == x0.Path && stageState(f) != ui.CheckOn {
			t.Fatalf("not staged: %+v", f)
		}
	}
	st.git.message = "Hourly forecast in the widget"
	tt.Frame()
	if err := tt.Click(a.tr("gitCommit")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if st.git.refusal != nil || st.status.Ahead != 3 || st.git.message != "" {
		t.Fatalf("commit: %+v %+v", st.git.refusal, st.status)
	}

	// push asks first, then publishes the branch
	if err := tt.Click(a.tr("gitPush") + " ↑3"); err != nil {
		t.Fatalf("%v: %v", err, tt.Texts())
	}
	tt.Frame()
	shot(t, tt, "git-push")
	if !st.git.pushAsk {
		t.Fatal("push didn't ask")
	}
	a.gitRun(key, x, "push", "push", map[string]any{"set_upstream": true}, "", func(hubapi.GitWriteResult) { st.git.pushAsk = false })
	if st.status.Upstream == "" || st.status.Ahead != 0 {
		t.Fatalf("push: %+v", st.status)
	}

	// branches: switch to main
	st.git.branchesOpen = true
	a.readBranches(key, x)
	tt.Frame()
	if len(st.git.branches) == 0 {
		t.Fatal("no branches")
	}
	shot(t, tt, "git-branches")
	a.gitRun(key, x, "switch:main", "switch", map[string]any{"branch": "main", "create": false}, "", func(hubapi.GitWriteResult) { st.git.branchesOpen = false })
	if st.status.Branch != "main" {
		t.Fatalf("switch: %+v", st.status)
	}

	// stashes: the list, then a refusal shown with git's words
	st.git.stashesOpen = true
	a.readStashes(key, x)
	tt.Frame()
	if !st.git.stashesRead || len(st.git.stashes) == 0 {
		t.Fatalf("stashes: %+v", st.git)
	}
	shot(t, tt, "git-stashes")
	st.git.stashesOpen = false
	a.gitRun(key, x, "stash:drop:9", "stash", map[string]any{"op": "drop", "index": 9, "oid": "x"}, "", nil)
	if st.git.refusal == nil || st.git.refusal.title != a.tr("gitRefusedGitStashStale") {
		t.Fatalf("refusal: %+v", st.git.refusal)
	}
	tt.Frame()
	shot(t, tt, "git-refusal")
	st.git.refusal = nil

	// history: a commit's files and diff
	st.git.tab = 1
	tt.Frame()
	if len(st.status.Commits) == 0 {
		t.Fatal("no commits")
	}
	st.git.commit = st.status.Commits[0].Hash
	tt.Frame()
	tt.Frame()
	if s := st.git.shows[st.git.commit]; s == nil || !s.loaded {
		t.Fatalf("show: %+v", s)
	}
	shot(t, tt, "git-history")
	tt.SetDark(true)
	shot(t, tt, "git-history-dark")
	_ = getJSONQuiet(gitMockURL + "/mock/git-write?reset=1")
}

// getJSONQuiet asks the mock for something, ignoring the answer.
func getJSONQuiet(u string) error {
	r, err := http.Get(u)
	if err == nil {
		r.Body.Close()
	}
	return err
}

func TestRefusalKey(t *testing.T) {
	a := New(newHost(), hubs.Load(filepath.Join(t.TempDir(), "hubs.json")), "en", "1.0.0")
	if r := a.gitRefusal(&hubapi.Error{Code: "git_no_upstream", Message: "no upstream"}); r.title != a.tr("gitRefusedGitNoUpstream") || r.detail != "no upstream" {
		t.Fatalf("%+v", r)
	}
	if r := a.gitRefusal(&hubapi.Error{Code: "rpc", Message: "unknown method git.stage"}); r.title != a.tr("gitRefusedOldNode") {
		t.Fatalf("%+v", r)
	}
	if st := stageState(hubapi.GitFile{Index: "M", Worktree: "M"}); st != ui.CheckMixed {
		t.Fatal("mixed", st)
	}
	if st := stageState(hubapi.GitFile{Index: "?", Worktree: "?"}); st != ui.CheckOff {
		t.Fatal("untracked", st)
	}
}

func TestFiles(t *testing.T) {
	a, host := signedIn(t, "en")
	x := weatherAgent(t, a)
	key := x.Key()
	a.Navigate("/agents/" + key)
	tt := ui.NewTester(a.View, 1280, 900)
	tt.Frame()
	if err := tt.Click(a.tr("paneFiles")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if !tt.HasText("README.md") || !tt.HasText("Sources") || tt.HasText(".gitignore") {
		t.Fatalf("listing: %v", tt.Texts())
	}
	// into a folder and back
	if err := tt.Click("Sources"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if !tt.HasText("App") {
		t.Fatalf("Sources: %v", tt.Texts())
	}
	if err := tt.Click("weather"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// a text file with its lines
	if err := tt.Click("Package.swift"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	fs := a.files(key)
	if fs.file == nil || !fs.file.loaded || !strings.Contains(fs.file.text, "PackageDescription") || !tt.HasText("import PackageDescription") {
		t.Fatalf("file: %+v", fs.file)
	}
	shot(t, tt, "files-text")
	// Markdown rendered
	if err := tt.Click("README.md"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	shot(t, tt, "files-markdown")
	tt.SetDark(true)
	shot(t, tt, "files-markdown-dark")
	tt.SetDark(false)
	// hidden files
	fs.hidden = true
	tt.Frame()
	if !tt.HasText(".gitignore") {
		t.Fatalf("hidden: %v", tt.Texts())
	}
	// a picture
	fs.dirs = append(fs.dirs, "/Users/me/code/weather/docs")
	tt.Frame()
	tt.Frame()
	if err := tt.Click("icon.png"); err != nil {
		t.Fatalf("%v: %v", err, tt.Texts())
	}
	tt.Frame()
	tt.Frame()
	if fs.file.image == nil {
		t.Fatalf("image: %+v", fs.file.content.Mime)
	}
	shot(t, tt, "files-image")
	// Save a Copy downloads it
	host.save = filepath.Join(t.TempDir(), "icon.png")
	a.saveCopy(key, x, fs.file.path)
	if b, err := os.ReadFile(host.save); err != nil || len(b) == 0 {
		t.Fatalf("saved: %v %d", err, len(b))
	}
	if sizeText(1536) != "1.5 KB" || sizeText(12) != "12 B" {
		t.Fatal(sizeText(1536))
	}
}

func TestGitMarks(t *testing.T) {
	gs := hubapi.GitStatus{IsRepo: true, Root: "/r", Files: []hubapi.GitFile{
		{Path: "a.go", Index: ".", Worktree: "M"}, {Path: "sub/b.go", Index: "A", Worktree: "."}, {Path: "sub/deep/c.go", Index: "?", Worktree: "?"}}}
	m := gitMarks(gs, "/r")
	if m["a.go"] != "M" || m["sub"] != "•" {
		t.Fatalf("root: %v", m)
	}
	m = gitMarks(gs, "/r/sub")
	if m["b.go"] != "A" || m["deep"] != "•" {
		t.Fatalf("sub: %v", m)
	}
}
