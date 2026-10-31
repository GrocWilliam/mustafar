package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

// project is one repository the forge works on. It embeds the forge for the shared config, Docker and agent slots.
type project struct {
	*forge
	name   string // owner/repo
	id     int64  // Gitea's repository id: stable and unique, it picks the staging port
	gitea  *gitea
	sshURL string // agents clone and push through it, inside the compose network
	ready  bool
	base   string // the repository's default branch, read on setup
	// merging serializes this project's merges, so each one is checked against the latest main.
	merging sync.Mutex
}

func newProject(f *forge, repo repoInfo) *project {
	return &project{
		forge:  f,
		name:   repo.FullName,
		id:     repo.ID,
		gitea:  newGitea(f.cfg, "/repos/"+repo.FullName),
		sshURL: f.cfg.SSHBase + repo.FullName + ".git",
	}
}

// poll fills the free agent slots: specs to plan first, then the `us-todo` US whose dependencies are done.
// Each unit of work runs in its own goroutine; the next poll skips what is already running.
func (o *project) poll() error {
	// Gitea may still be starting, or the repository was just created: keep retrying setup instead of dying.
	if !o.ready {
		if err := o.gitea.ensureLabels(); err != nil {
			return err
		}
		base, err := o.gitea.defaultBranch()
		if err != nil {
			return err
		}
		if err := o.gitea.protect(base); err != nil {
			return fmt.Errorf("protecting %s: %w", base, err)
		}
		if err := o.recover(); err != nil {
			return err
		}
		o.base, o.ready = base, true
	}
	if err := o.closeFinishedSpecs(); err != nil {
		return err
	}
	specs, err := o.gitea.issuesWith("spec")
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if !o.start(spec.Number, func() error { return o.planSpec(spec) }) {
			return nil
		}
	}
	todo, err := o.gitea.issuesWith("us-todo")
	if err != nil {
		return err
	}
	ready, err := o.readyStories(todo)
	for _, story := range ready {
		if !o.start(story.Number, func() error { return o.handle(story) }) {
			break
		}
	}
	return err
}

func (o *project) start(number int, work func() error) bool {
	return o.forge.start(fmt.Sprintf("%s#%d", o.name, number), work)
}

func (o *project) isRunning(number int) bool {
	return o.forge.isRunning(fmt.Sprintf("%s#%d", o.name, number))
}

// recover re-queues US left in progress or in review by a previous run: their pipeline died with it.
func (o *project) recover() error {
	for _, label := range []string{"us-in-progress", "us-review"} {
		stale, err := o.gitea.issuesWith(label)
		if err != nil {
			return err
		}
		for _, is := range stale {
			if err := o.gitea.move(is.Number, label, "us-todo"); err != nil {
				return err
			}
			if _, err := o.gitea.comment(is.Number, "♻️ Orchestrator restarted: this US is back in the queue and starts over."); err != nil {
				return err
			}
		}
	}
	return nil
}

// handle runs one US through the whole pipeline, until it is merged or blocked.
func (o *project) handle(is issue) error {
	log.Printf("%s: picking up US #%d", o.name, is.Number)
	label := "us-todo"
	if err := o.gitea.move(is.Number, label, "us-in-progress"); err != nil {
		return err
	}
	label = "us-in-progress"
	if err := o.deliver(is, &label); err != nil {
		log.Printf("%s: US #%d blocked: %v", o.name, is.Number, err)
		return o.block(is.Number, label, err.Error())
	}
	log.Printf("%s: US #%d merged", o.name, is.Number)
	return nil
}

func (o *project) block(number int, from, reason string) error {
	if err := o.gitea.move(number, from, "us-blocked"); err != nil {
		return err
	}
	hint := "\n\nFix the US, then set the `us-todo` label again."
	switch from {
	case "spec":
		hint = "\n\nFix the spec, then set the `spec` label again."
	case "spec-active":
		hint = "" // stopSpec's reason says how to resume
	}
	_, err := o.gitea.comment(number, "⛔ Blocked: "+strings.TrimSpace(reason)+hint)
	return err
}
