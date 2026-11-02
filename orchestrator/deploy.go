package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Staging deployment, run by the orchestrator itself: it already holds the Docker socket, and a CI runner given
// the socket would hand host root to any pull request, since Gitea runs a PR's own workflow files.

const (
	appPort       = "8080" // inside the container, for every project
	healthTimeout = time.Minute
)

// staging names the project's staging container.
func (o *project) staging() string {
	return "forge-staging-" + strings.ReplaceAll(o.name, "/", "-")
}

// stagingPort is the project's port on localhost: repository ids are unique and never reused, so it is stable.
func (o *project) stagingPort() string {
	return fmt.Sprint(9000 + o.id)
}

// deploy builds the repository at sha and runs it as the staging container, then waits for GET /health to answer 200.
// It reports false, deploying nothing, when the repository has no Dockerfile yet.
func (o *project) deploy(sha string) (bool, error) {
	files, err := o.gitea.tree(sha)
	if err != nil {
		return false, err
	}
	if _, ok := files["Dockerfile"]; !ok {
		return false, nil
	}
	archive, err := o.gitea.archive(sha)
	if err != nil {
		return true, err
	}
	defer archive.Close()
	context, writer := io.Pipe()
	defer context.Close() // unblocks the copy if the build stops reading early
	go func() { writer.CloseWithError(stripTopDir(archive, writer)) }()

	// shortcut: one image per deployed commit and none is ever pruned, add a cleanup if the disk fills up
	image := "forge-app/" + strings.ToLower(o.name) + ":" + sha[:12]
	if err := o.docker.build(context, image); err != nil {
		return true, err
	}
	if err := o.docker.serve(o.staging(), image, o.cfg.AgentNetwork, appPort, o.stagingPort()); err != nil {
		return true, err
	}
	return true, waitHealthy("http://"+o.staging()+":"+appPort+"/health", healthTimeout)
}

func waitHealthy(url string, timeout time.Duration) error {
	client := http.Client{Timeout: 5 * time.Second}
	last := errors.New("no answer")
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(time.Second) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			err = fmt.Errorf("status %s", resp.Status)
		}
		last = err
	}
	return fmt.Errorf("GET %s did not answer 200 within %s (last: %v)", url, timeout, last)
}

// stripTopDir rewrites Gitea's .tar.gz archive, where every path sits under `<repo>/`, as a plain tar rooted at the repository.
func stripTopDir(archive io.Reader, w io.Writer) error {
	unzipped, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	in, out := tar.NewReader(unzipped), tar.NewWriter(w)
	for {
		header, err := in.Next()
		if err == io.EOF {
			return out.Close()
		} else if err != nil {
			return err
		}
		_, rest, ok := strings.Cut(header.Name, "/")
		if !ok || rest == "" { // pax global header, top-level directory
			continue
		}
		header.Name = rest
		if err := out.WriteHeader(header); err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			return err
		}
	}
}

// rollback undoes a merge whose deployment failed: revert it on main, redeploy the previous main, reopen the US.
// It returns the reason the US is blocked.
func (o *project) rollback(is issue, merged, previous string, cause error) error {
	reason := fmt.Sprintf("the pull request was merged, but the staging deployment failed: `%v`", cause)
	if err := o.revert(merged, fmt.Sprintf("Revert %s (#%d): staging deployment failed", is.Title, is.Number)); err != nil {
		return fmt.Errorf("%s. Reverting the merge failed too, so main is broken: `%v`", reason, err)
	}
	reason += ". The merge was reverted on main"
	if deployed, err := o.deploy(previous); err != nil {
		reason += fmt.Sprintf("; redeploying the previous version failed too: `%v`", err)
	} else if !deployed {
		if err := o.docker.remove(o.staging()); err != nil {
			reason += fmt.Sprintf("; removing the broken staging container failed: `%v`", err)
		}
	} else {
		reason += ", and staging runs the previous version again"
	}
	if err := o.gitea.reopen(is.Number); err != nil {
		return err
	}
	return errors.New(reason + ".")
}

// revert undoes sha on main through a pull request, since nobody may push to main: a one-shot agent container
// pushes a `git revert` to its own branch with the deploy key (plain git, no Claude), and the orchestrator merges it.
func (o *project) revert(sha, title string) error {
	key, err := os.ReadFile(o.cfg.DeployKeyFile)
	if err != nil {
		return err
	}
	branch := "revert/" + sha[:12]
	env := []string{"ROLE=revert", "ISSUE_NUMBER=0", "REPO_URL=" + o.sshURL, "DEPLOY_KEY=" + string(key), "REVERT_SHA=" + sha, "REVERT_BRANCH=" + branch}
	id, err := o.docker.run(o.cfg.AgentImage, o.cfg.AgentNetwork, env)
	if id != "" {
		defer o.docker.remove(id)
	}
	if err != nil {
		return err
	}
	code, err := o.docker.wait(id)
	if err != nil {
		return err
	}
	if code != 0 {
		var stderr []byte
		if logs, err := o.docker.logs(id, "stderr=1&tail=10"); err == nil {
			stderr, _ = io.ReadAll(logs)
			logs.Close()
		}
		return fmt.Errorf("git revert exited with %d: %s", code, strings.TrimSpace(string(stderr)))
	}
	pr, err := o.gitea.openPR(title, "Reverts "+sha+": its staging deployment failed.", branch, o.base)
	if err != nil {
		return err
	}
	return o.gitea.merge(pr.Number, title, pr.Head.SHA)
}
