package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/nwverse/persona/desktop/internal/hubapi"
)

// The Changes pane's Git writes and history, on a hub with git_write (docs/protocol/protocol.md "git/stage … git/stash"):
// stage a file or all, commit, push (asked first), switch or make a branch, stash, the branch's pull request; and the
// history with each commit's files and diff. A refusal says what happened, git's own words and the files it is about.

// gitState is the writes' and the history's part of an agent's changes.
type gitState struct {
	tab     int // the list: changes or history
	message string
	busy    string
	refusal *gitRefusal
	pushAsk bool

	older       []hubapi.GitCommit
	noMore      bool
	loadingMore bool
	commit      string // the commit chosen in the history
	shows       map[string]*showState

	branchesOpen bool
	branches     []hubapi.GitBranch
	branchesErr  string
	branchQuery  string
	newBranch    string

	stashesOpen    bool
	stashes        []hubapi.GitStash
	stashesRead    bool
	stashMessage   string
	stashUntracked bool
	dropping       string

	pr       *hubapi.GitPR
	prRead   bool
	prErr    string
	prAsked  bool
	prCreate bool
	prTitle  string
	prBody   string
	prDraft  bool
}

type showState struct {
	show    hubapi.GitShow
	loaded  bool
	loading bool
	err     string
}

type gitRefusal struct {
	title  string
	detail string
	files  []string
}

func (a *App) gitRefusal(err error) *gitRefusal {
	var e *hubapi.Error
	if errors.As(err, &e) {
		title := a.tr("gitRefused")
		switch e.Code {
		case "git_not_repo":
			title = a.tr("gitRefusedGitNotRepo")
		case "git_detached":
			title = a.tr("gitRefusedGitDetached")
		case "git_nothing_staged":
			title = a.tr("gitRefusedGitNothingStaged")
		case "git_hook_failed":
			title = a.tr("gitRefusedGitHookFailed")
		case "git_identity":
			title = a.tr("gitRefusedGitIdentity")
		case "git_no_upstream":
			title = a.tr("gitRefusedGitNoUpstream")
		case "git_no_remote":
			title = a.tr("gitRefusedGitNoRemote")
		case "git_push_rejected":
			title = a.tr("gitRefusedGitPushRejected")
		case "git_auth":
			title = a.tr("gitRefusedGitAuth")
		case "git_conflict":
			title = a.tr("gitRefusedGitConflict")
		case "git_busy":
			title = a.tr("gitRefusedGitBusy")
		case "git_no_branch":
			title = a.tr("gitRefusedGitNoBranch")
		case "git_no_stash":
			title = a.tr("gitRefusedGitNoStash")
		case "git_stash_stale":
			title = a.tr("gitRefusedGitStashStale")
		case "git_stash_unsupported":
			title = a.tr("gitRefusedGitStashUnsupported")
		case "git_not_pushed":
			title = a.tr("gitRefusedGitNotPushed")
		case "git_no_pr":
			title = a.tr("gitRefusedGitNoPr")
		case "git_not_github":
			title = a.tr("gitRefusedGitNotGithub")
		case "no_gh":
			title = a.tr("gitRefusedNoGh")
		case "not_signed_in":
			title = a.tr("gitRefusedNotSignedIn")
		default:
			// a node from before the writes doesn't know the method: Persona there needs updating
			if strings.Contains(e.Message, "unknown method git.") {
				title = a.tr("gitRefusedOldNode")
			}
		}
		return &gitRefusal{title: title, detail: e.Message, files: e.Files}
	}
	return &gitRefusal{title: a.tr("gitRefused"), detail: a.errorText(err)}
}

func (a *App) gitWritable() bool { return slices.Contains(a.conn.data.Hello.Features, "git_write") }

// gitRun runs one write at a time for an agent's folder; the status it answers replaces the one shown.
func (a *App) gitRun(key string, x hubapi.Session, what, op string, body map[string]any, done string, after func(hubapi.GitWriteResult)) {
	st := a.changes(key)
	g := &st.git
	if g.busy != "" {
		return
	}
	g.busy = what
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		r, err := c.client.GitWrite(ctx, x.MachineID, op, x.Cwd, body)
		a.Host.Post(func() {
			g.busy = ""
			if !a.live(c) {
				return
			}
			if err != nil {
				g.refusal = a.gitRefusal(err)
				return
			}
			a.setGitStatus(st, r.Status)
			if done != "" {
				a.toast(done)
			}
			if after != nil {
				after(r)
			}
		})
	})
}

// setGitStatus shows a status read or answered; a new HEAD drops the older pages and the commits' reads.
func (a *App) setGitStatus(st *changesState, gs hubapi.GitStatus) {
	if head := func(s hubapi.GitStatus) string {
		if s.Head != nil {
			return s.Head.Hash
		}
		return ""
	}; head(gs) != head(st.status) {
		st.git.older, st.git.noMore = nil, false
	}
	st.status, st.loaded, st.diffs = gs, true, map[string]*diffState{}
	if !slices.ContainsFunc(gs.Files, func(f hubapi.GitFile) bool { return f.Path == st.chosen }) {
		st.chosen = ""
		if len(gs.Files) > 0 {
			st.chosen = gs.Files[0].Path
		}
	}
	st.git.prAsked = false
}

// stageState is whether a file's changes are all staged, some or none.
func stageState(f hubapi.GitFile) ui.CheckState {
	inIndex := f.Index != "." && f.Index != "?" && f.Index != ""
	inTree := f.Worktree != "." && f.Worktree != "?" && f.Worktree != "" || f.Index == "?"
	switch {
	case inIndex && !inTree:
		return ui.CheckOn
	case inIndex:
		return ui.CheckMixed
	}
	return ui.CheckOff
}

// stageBox is a changed file's check box: on stages all of it, off unstages it.
func (a *App) stageBox(c *ui.Context, key string, x hubapi.Session, f hubapi.GitFile) {
	g := &a.changes(key).git
	state := stageState(f)
	box := ui.CheckboxState(c.Key("stage-"+f.Path), &state, "").Label(a.tr("gitStage", "path", f.Path)).Disabled(g.busy != "")
	if box.Changed() {
		a.gitRun(key, x, "stage:"+f.Path, "stage", map[string]any{"paths": []string{f.Path}, "unstage": state == ui.CheckOff}, "", nil)
	}
}

// gitBar is the writes above the list: Stage all / Unstage all, and the commit box.
func (a *App) gitBar(c *ui.Context, p palette, key string, x hubapi.Session) {
	st := a.changes(key)
	g := &st.git
	gs := st.status
	all := len(gs.Files) > 0 && !slices.ContainsFunc(gs.Files, func(f hubapi.GitFile) bool { return stageState(f) != ui.CheckOn })
	staged := slices.ContainsFunc(gs.Files, func(f hubapi.GitFile) bool { return stageState(f) != ui.CheckOff })
	ui.Column(c).Padding(8).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(p.line).Children(func() {
		label := a.tr("gitStageAll")
		if all {
			label = a.tr("gitUnstageAll")
		}
		paths := make([]string, len(gs.Files))
		for i, f := range gs.Files {
			paths[i] = f.Path
		}
		if ui.Button(c, label).FontSize(small).Disabled(g.busy != "").Clicked() {
			a.gitRun(key, x, "stage:all", "stage", map[string]any{"paths": paths, "unstage": all}, "", nil)
		}
		ui.TextArea(c.Key("commit-msg-"+key), &g.message).Lines(2, 6).Placeholder(a.tr("gitCommitPlaceholder")).Label(a.tr("gitCommitPlaceholder")).FontSize(small)
		btn := ui.PrimaryButton(c, a.tr("gitCommit")).Disabled(g.busy != "" || !staged || strings.TrimSpace(g.message) == "")
		if g.busy == "commit" {
			btn.Disabled(true)
		}
		if btn.Clicked() {
			msg := strings.TrimSpace(g.message)
			a.gitRun(key, x, "commit", "commit", map[string]any{"message": msg}, "", func(r hubapi.GitWriteResult) {
				g.message = ""
				if r.Commit != nil {
					a.toast(a.tr("gitCommitted", "short", r.Commit.Short))
				}
			})
		}
	})
}

// gitActions are the header's buttons: Branches, Stashes, Push.
func (a *App) gitActions(c *ui.Context, p palette, key string, x hubapi.Session) {
	st := a.changes(key)
	g := &st.git
	gs := st.status
	if ui.Button(c, a.tr("gitBranches")).Disabled(g.busy != "").Clicked() {
		g.branchesOpen, g.branchQuery, g.newBranch, g.branchesErr = true, "", "", ""
		a.readBranches(key, x)
	}
	stashes := a.tr("gitStashes")
	if gs.Stashes > 0 {
		stashes += " (" + itoa(gs.Stashes) + ")"
	}
	if ui.Button(c, stashes).Disabled(g.busy != "").Clicked() {
		g.stashesOpen, g.stashMessage, g.stashUntracked, g.dropping = true, "", false, ""
		a.readStashes(key, x)
	}
	fresh := gs.Upstream == "" || gs.Gone
	if gs.Branch != "" && (gs.Ahead > 0 || fresh) {
		label := a.tr("gitPush")
		if gs.Ahead > 0 {
			label += " ↑" + itoa(gs.Ahead)
		}
		if ui.Button(c, label).Disabled(g.busy != "").Clicked() {
			g.pushAsk = true
		}
	}
}

// gitDialogs are the Changes pane's dialogs: the refusal, Push, Branches, Stashes and New pull request.
func (a *App) gitDialogs(c *ui.Context, p palette, key string, x hubapi.Session) {
	st := a.changes(key)
	g := &st.git
	gs := st.status
	if g.refusal != nil {
		open := true
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(520).Padding(20).Gap(10).Children(func() {
				ui.Text(c, g.refusal.title).FontSize(heading).Bold()
				if g.refusal.detail != "" {
					ui.Scroll(c.Key("refusal")).MaxHeight(240).Children(func() {
						ui.Text(c, g.refusal.detail).Font("monospace").FontSize(small).TextColor(p.ink2).Selectable()
					})
				}
				for i, f := range g.refusal.files {
					ui.Text(c.Key("rf"+itoa(i)), f).Font("monospace").FontSize(small)
				}
				ui.Row(c).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("done")).Clicked() {
						open = false
					}
				})
			})
		})
		if !open {
			g.refusal = nil
		}
		return
	}
	if g.pushAsk {
		fresh := gs.Upstream == "" || gs.Gone
		ui.Modal(c, &g.pushAsk, func() {
			ui.Column(c).Width(460).Padding(20).Gap(10).Children(func() {
				ui.Text(c, a.tr("gitPushTitle", "branch", gs.Branch)).FontSize(heading).Bold()
				body := a.tr("gitPushBody", "count", itoa(gs.Ahead), "upstream", gs.Upstream)
				if fresh {
					body = a.tr("gitPushFresh", "branch", gs.Branch)
				}
				ui.Text(c, body).TextColor(p.ink2)
				ui.Row(c).Gap(8).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, a.tr("cancel")).Clicked() {
						g.pushAsk = false
					}
					if ui.PrimaryButton(c, a.tr("gitPush")).Clicked() {
						g.pushAsk = false
						a.gitRun(key, x, "push", "push", map[string]any{"set_upstream": fresh}, a.tr("gitPushed", "branch", gs.Branch), nil)
					}
				})
			})
		})
	}
	if g.branchesOpen {
		a.branchesDialog(c, p, key, x)
	}
	if g.stashesOpen {
		a.stashesDialog(c, p, key, x)
	}
	if g.prCreate {
		a.prDialog(c, p, key, x)
	}
}

func (a *App) readBranches(key string, x hubapi.Session) {
	g := &a.changes(key).git
	c := a.conn
	g.branches = nil
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		list, err := c.client.GitBranches(ctx, x.MachineID, x.Cwd)
		a.Host.Post(func() {
			if !a.live(c) {
				return
			}
			if err != nil {
				g.branchesErr = a.errorText(err)
				return
			}
			g.branches = list
		})
	})
}

func (a *App) branchesDialog(c *ui.Context, p palette, key string, x hubapi.Session) {
	g := &a.changes(key).git
	ui.Modal(c, &g.branchesOpen, func() {
		ui.Column(c).Width(520).Padding(20).Gap(10).Children(func() {
			ui.Text(c, a.tr("gitBranches")).FontSize(heading).Bold()
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c.Key("new-branch"), &g.newBranch).Placeholder(a.tr("gitNewBranch")).Label(a.tr("gitNewBranch")).Font("monospace").FontSize(small).Grow(1)
				name := strings.TrimSpace(g.newBranch)
				if ui.Button(c, a.tr("gitCreate")).Disabled(g.busy != "" || name == "").Clicked() {
					a.gitRun(key, x, "switch:"+name, "switch", map[string]any{"branch": name, "create": true}, a.tr("gitCreated", "branch", name),
						func(hubapi.GitWriteResult) { g.branchesOpen = false })
				}
			})
			ui.SearchField(c.Key("branch-q"), &g.branchQuery).Placeholder(a.tr("gitFindBranch"))
			switch {
			case g.branchesErr != "":
				ui.Text(c, g.branchesErr).FontSize(small).TextColor(p.red)
			case g.branches == nil:
				ui.Spinner(c).Margin(12, ui.Auto)
			}
			q := strings.ToLower(strings.TrimSpace(g.branchQuery))
			now := time.Now()
			ui.Scroll(c.Key("branches")).MaxHeight(360).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					for i, b := range g.branches {
						if q != "" && !strings.Contains(strings.ToLower(b.Name), q) {
							continue
						}
						ui.Row(c.Key("br"+itoa(i))).Padding(6, 8).Gap(10).Radius(rowRadius).Children(func() {
							ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
								name := ui.Text(c, b.Name).Font("monospace").FontSize(small).SingleLine()
								if b.Current {
									name.Bold()
								}
								ui.Text(c, b.Subject+" · "+a.ago(b.Date, now)).FontSize(11).TextColor(p.ink3).SingleLine()
							})
							if b.Current {
								a.pill(c, a.tr("gitCurrent"), p.green)
								return
							}
							target := b.Name
							if b.Remote {
								target = b.Name[strings.Index(b.Name, "/")+1:]
							}
							if ui.Button(c, a.tr("gitSwitch")).FontSize(small).Padding(2, 8).Disabled(g.busy != "").Clicked() {
								a.gitRun(key, x, "switch:"+target, "switch", map[string]any{"branch": target, "create": false},
									a.tr("gitSwitched", "branch", target), func(hubapi.GitWriteResult) { g.branchesOpen = false })
							}
						})
					}
				})
			})
		})
	})
}

func (a *App) readStashes(key string, x hubapi.Session) {
	g := &a.changes(key).git
	g.stashesRead = false
	a.gitRun(key, x, "stash:list", "stash", map[string]any{"op": "list"}, "", func(r hubapi.GitWriteResult) {
		g.stashes, g.stashesRead = r.Stashes, true
	})
}

func (a *App) stashesDialog(c *ui.Context, p palette, key string, x hubapi.Session) {
	g := &a.changes(key).git
	ui.Modal(c, &g.stashesOpen, func() {
		ui.Column(c).Width(560).Padding(20).Gap(10).Children(func() {
			ui.Text(c, a.tr("gitStashes")).FontSize(heading).Bold()
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c.Key("stash-msg"), &g.stashMessage).Placeholder(a.tr("gitStashMessage")).Label(a.tr("gitStashMessage")).Grow(1)
				if ui.Button(c, a.tr("gitStash")).Disabled(g.busy != "" || len(a.changes(key).status.Files) == 0).Clicked() {
					a.gitRun(key, x, "stash:push", "stash", map[string]any{"op": "push", "message": strings.TrimSpace(g.stashMessage), "untracked": g.stashUntracked},
						a.tr("gitStashed"), func(r hubapi.GitWriteResult) { g.stashes, g.stashMessage = r.Stashes, "" })
				}
			})
			ui.Checkbox(c.Key("stash-untracked"), &g.stashUntracked, a.tr("gitStashUntracked"))
			if !g.stashesRead {
				ui.Spinner(c).Margin(12, ui.Auto)
			} else if len(g.stashes) == 0 {
				ui.Text(c, a.tr("gitNoStashes")).FontSize(small).TextColor(p.ink3)
			}
			now := time.Now()
			ui.Scroll(c.Key("stashes")).MaxHeight(320).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					for _, e := range g.stashes {
						a.stashRow(c, p, key, x, e, now)
					}
				})
			})
		})
	})
}

func (a *App) stashRow(c *ui.Context, p palette, key string, x hubapi.Session, e hubapi.GitStash, now time.Time) {
	g := &a.changes(key).git
	id := itoa(e.Index) + ":" + e.Oid
	// an older node names a stash by its place alone, which may have moved: those are only shown
	safe := len(e.Oid) == 40 || len(e.Oid) == 64
	act := func(op, done string) {
		a.gitRun(key, x, "stash:"+op+":"+itoa(e.Index), "stash", map[string]any{"op": op, "index": e.Index, "oid": e.Oid}, done,
			func(r hubapi.GitWriteResult) { g.stashes, g.dropping = r.Stashes, "" })
	}
	ui.Row(c.Key("stash-"+id)).Padding(6, 8).Gap(8).Radius(rowRadius).Background(p.inset).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, e.Message).FontSize(small).SingleLine()
			ui.Text(c, "stash@{"+itoa(e.Index)+"} · "+a.ago(e.Date, now)).FontSize(11).TextColor(p.ink3).SingleLine()
		})
		if g.dropping == id {
			if ui.Button(c, a.tr("cancel")).FontSize(small).Padding(2, 8).Clicked() {
				g.dropping = ""
			}
			if ui.Button(c, a.tr("gitDropConfirm")).FontSize(small).Padding(2, 8).TextColor(p.red).Disabled(g.busy != "").Clicked() {
				act("drop", a.tr("gitDropped"))
			}
			return
		}
		busy := g.busy != "" || !safe
		if ui.Button(c, a.tr("gitApply")).FontSize(small).Padding(2, 8).Disabled(busy).Clicked() {
			act("apply", a.tr("gitApplied"))
		}
		if ui.Button(c, a.tr("gitPop")).FontSize(small).Padding(2, 8).Disabled(busy).Clicked() {
			act("pop", a.tr("gitPopped"))
		}
		if ui.Button(c, a.tr("gitDrop")).FontSize(small).Padding(2, 8).TextColor(p.red).Disabled(busy).Clicked() {
			g.dropping = id
		}
	})
}

// readPR reads the branch's pull request once per status.
func (a *App) readPR(key string, x hubapi.Session) {
	g := &a.changes(key).git
	if g.prAsked {
		return
	}
	g.prAsked = true
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pr, err := c.client.GitPR(ctx, x.MachineID, x.Cwd)
		a.Host.Post(func() {
			if !a.live(c) {
				return
			}
			g.prRead, g.pr, g.prErr = true, pr, ""
			if err != nil {
				g.prErr = a.gitRefusal(err).title
			}
		})
	})
}

// prCard is the branch's pull request: its title, state and checks, Open and Merge; or New pull request once the branch is pushed.
func (a *App) prCard(c *ui.Context, p palette, key string, x hubapi.Session) {
	st := a.changes(key)
	g := &st.git
	gs := st.status
	if gs.Branch == "" {
		return
	}
	a.readPR(key, x)
	if !g.prRead {
		return
	}
	ui.Column(c).Margin(6).Padding(10).Gap(6).Radius(rowRadius).Border(1, p.line).Background(p.surface).Children(func() {
		pr := g.pr
		switch {
		case g.prErr != "":
			ui.Text(c, g.prErr).FontSize(11).TextColor(p.ink3)
			return
		case pr == nil:
			pushed := gs.Upstream != "" && !gs.Gone && gs.Ahead == 0
			ui.Text(c, a.tr("gitNoPR")).FontSize(small).TextColor(p.ink2)
			if ui.Button(c, a.tr("gitNewPR")).FontSize(small).Disabled(!pushed || g.busy != "").Tooltip(a.tr("gitRefusedGitNotPushed")).Clicked() {
				subject := ""
				if gs.Head != nil {
					subject = gs.Head.Subject
				}
				g.prCreate, g.prTitle, g.prBody, g.prDraft = true, subject, "", false
			}
			return
		}
		ui.Text(c, "#"+itoa(pr.Number)+" "+pr.Title).FontSize(small).Bold().MaxLines(2)
		state, col := a.prState(p, pr)
		ui.Row(c).Gap(6).Wrap().Children(func() {
			a.pill(c, state, col)
			if pr.ChecksState != "" {
				ccol, label := p.ink2, pr.ChecksState
				switch pr.ChecksState {
				case "passing":
					ccol, label = p.green, a.tr("gitChecksPassing")
				case "failing":
					ccol, label = p.red, a.tr("gitChecksFailing")
				case "pending":
					ccol, label = p.orange, a.tr("gitChecksPending")
				}
				a.pill(c, label, ccol)
			}
		})
		for i, ch := range pr.Checks {
			if ch.State == "pass" || ch.State == "skipped" {
				continue
			}
			ui.Text(c.Key("chk"+itoa(i)), "· "+ch.Name+" — "+ch.State).FontSize(11).TextColor(p.ink2).SingleLine()
		}
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, a.tr("gitOpenPR")).FontSize(small).Padding(2, 8).Clicked() {
				a.Host.OpenURL(pr.URL)
			}
			if pr.State == "OPEN" && !pr.Draft {
				ready := slices.Contains([]string{"CLEAN", "UNSTABLE", "HAS_HOOKS"}, pr.MergeState)
				label := a.tr("gitMerge")
				if !ready {
					label = a.tr("gitAutoMerge")
				}
				if !pr.AutoMerge && ui.Button(c, label).FontSize(small).Padding(2, 8).Disabled(g.busy != "").Clicked() {
					a.mergePR(key, x, pr.Number, !ready)
				}
			}
		})
	})
}

func (a *App) prState(p palette, pr *hubapi.GitPR) (string, ui.Color) {
	switch {
	case pr.State == "MERGED":
		return a.tr("gitPRMerged"), p.accent
	case pr.State == "CLOSED":
		return a.tr("gitPRClosed"), p.ink3
	case pr.Draft:
		return a.tr("gitPRDraft"), p.ink2
	case pr.AutoMerge:
		return a.tr("gitPRAuto"), p.green
	case pr.Review == "APPROVED":
		return a.tr("gitPRApproved"), p.green
	case pr.Review == "CHANGES_REQUESTED":
		return a.tr("gitPRChanges"), p.orange
	}
	return a.tr("gitPROpen"), p.green
}

func (a *App) mergePR(key string, x hubapi.Session, number int, auto bool) {
	g := &a.changes(key).git
	g.busy = "pr"
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		pr, err := c.client.GitPRMerge(ctx, x.MachineID, x.Cwd, number, auto)
		a.Host.Post(func() {
			g.busy = ""
			if !a.live(c) {
				return
			}
			if err != nil {
				g.refusal = a.gitRefusal(err)
				return
			}
			g.pr = pr
			if auto {
				a.toast(a.tr("gitAutoMergeOn"))
			} else {
				a.toast(a.tr("gitMerged"))
			}
		})
	})
}

func (a *App) prDialog(c *ui.Context, p palette, key string, x hubapi.Session) {
	g := &a.changes(key).git
	ui.Modal(c, &g.prCreate, func() {
		ui.Column(c).Width(560).Padding(20).Gap(10).Children(func() {
			ui.Text(c, a.tr("gitNewPR")).FontSize(heading).Bold()
			ui.TextInput(c.Key("pr-title"), &g.prTitle).Label(a.tr("gitPRTitle")).Placeholder(a.tr("gitPRTitle"))
			ui.TextArea(c.Key("pr-body"), &g.prBody).Lines(4, 10).Label(a.tr("gitPRBody")).Placeholder(a.tr("gitPRBody"))
			ui.Checkbox(c.Key("pr-draft"), &g.prDraft, a.tr("gitPRDraftBox"))
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c, a.tr("cancel")).Clicked() {
					g.prCreate = false
				}
				if ui.PrimaryButton(c, a.tr("gitCreatePR")).Disabled(g.busy != "" || strings.TrimSpace(g.prTitle) == "").Clicked() {
					a.createPR(key, x)
				}
			})
		})
	})
}

func (a *App) createPR(key string, x hubapi.Session) {
	g := &a.changes(key).git
	g.busy = "pr"
	c := a.conn
	title, body, draft := strings.TrimSpace(g.prTitle), strings.TrimSpace(g.prBody), g.prDraft
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		pr, err := c.client.GitPRCreate(ctx, x.MachineID, x.Cwd, title, body, draft)
		a.Host.Post(func() {
			g.busy = ""
			if !a.live(c) {
				return
			}
			if err != nil {
				g.refusal = a.gitRefusal(err)
				return
			}
			g.pr, g.prCreate = pr, false
		})
	})
}

// historyList is the history's commits, older pages on demand.
func (a *App) historyList(c *ui.Context, p palette, key string, x hubapi.Session) {
	st := a.changes(key)
	g := &st.git
	commits := append(slices.Clone(st.status.Commits), g.older...)
	now := time.Now()
	if len(commits) == 0 {
		ui.Text(c, a.tr("gitNoCommits")).FontSize(small).TextColor(p.ink3).Padding(8)
	}
	for _, cm := range commits {
		on := cm.Hash == g.commit
		bg := ui.Transparent
		if on {
			bg = p.hover
		}
		row := ui.ButtonBase(c.Key("cm-"+cm.Hash)).Padding(6, 8).Radius(rowRadius).Gap(2).Background(bg).Column().AlignItems(ui.Stretch).Label(cm.Subject)
		row.Children(func() {
			ui.Text(c, cm.Subject).FontSize(small).SingleLine()
			line := cm.Short
			if cm.Author != "" {
				line += " · " + cm.Author
			}
			if cm.Date != "" {
				line += " · " + a.ago(cm.Date, now)
			}
			ui.Text(c, line).FontSize(11).TextColor(p.ink3).SingleLine()
		})
		if row.Hovered() && !on {
			row.Background(p.hover)
		}
		if row.Clicked() {
			g.commit = cm.Hash
		}
	}
	if !g.noMore && len(st.status.Commits) >= 30 {
		if ui.Button(c, a.tr("gitOlder")).FontSize(small).Margin(6).Disabled(g.loadingMore).Clicked() {
			a.loadOlderCommits(key, x)
		}
	}
}

func (a *App) loadOlderCommits(key string, x hubapi.Session) {
	st := a.changes(key)
	g := &st.git
	g.loadingMore = true
	skip := len(st.status.Commits) + len(g.older)
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		list, more, err := c.client.GitLog(ctx, x.MachineID, x.Cwd, skip, 50)
		a.Host.Post(func() {
			g.loadingMore = false
			if !a.live(c) {
				return
			}
			if err != nil {
				a.toast(a.errorText(err))
				return
			}
			g.older = append(g.older, list...)
			g.noMore = !more
		})
	})
}

func (a *App) readShow(key string, x hubapi.Session, hash string) *showState {
	g := &a.changes(key).git
	if g.shows == nil {
		g.shows = map[string]*showState{}
	}
	s := g.shows[hash]
	if s == nil {
		s = &showState{}
		g.shows[hash] = s
	}
	if s.loaded || s.loading || s.err != "" {
		return s
	}
	s.loading = true
	c := a.conn
	a.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		show, err := c.client.GitShow(ctx, x.MachineID, x.Cwd, hash)
		a.Host.Post(func() {
			s.loading = false
			if err != nil {
				s.err = a.errorText(err)
				return
			}
			s.show, s.loaded = show, true
		})
	})
	return s
}

// commitView is a commit: its message, author and files, then its diff.
func (a *App) commitView(c *ui.Context, p palette, key string, x hubapi.Session, hash string) {
	s := a.readShow(key, x, hash)
	switch {
	case s.err != "":
		a.empty(c, p, "warning", s.err, "")
		return
	case !s.loaded:
		ui.Spinner(c).Margin(ui.Auto)
		return
	}
	cm := s.show.Commit
	ui.Column(c).Padding(10, 16).Gap(4).BorderWidth(0, 0, 1, 0).BorderColor(p.line).Children(func() {
		ui.Text(c, cm.Subject).Bold().Selectable()
		line := cm.Short
		if cm.Author != "" {
			line += " · " + cm.Author
		}
		if cm.Date != "" {
			line += " · " + a.ago(cm.Date, time.Now())
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, line).FontSize(small).TextColor(p.ink2).Grow(1).SingleLine()
			if ui.Button(c, a.tr("gitCopyHash")).FontSize(small).Padding(2, 8).Clicked() {
				c.WriteClipboard(cm.Hash)
				a.toast(a.tr("gitHashCopied"))
			}
		})
		ui.Text(c, a.tr("gitCommitFiles", "count", itoa(len(s.show.Files)))).FontSize(small).TextColor(p.ink3)
	})
	a.diffBody(c, p, "show-"+key+"/"+hash, s.show.Diff, s.show.Truncated, true)
}
