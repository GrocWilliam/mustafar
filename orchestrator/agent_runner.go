package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	maxSteps  = 15               // steps shown in the progress comment
	editEvery = 15 * time.Second // between progress comment edits, to avoid hammering Gitea
)

// streamEvent is the subset of a Claude Code stream-json line the orchestrator reads.
type streamEvent struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	} `json:"message"`
	Subtype      string  `json:"subtype"` // on a result: "success", or why the run stopped ("error_max_turns"...)
	Result       string  `json:"result"`
	NumTurns     int     `json:"num_turns"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// parse decodes one stream-json line; anything unreadable becomes an empty event.
func parse(line []byte) streamEvent {
	var event streamEvent
	_ = json.Unmarshal(line, &event)
	return event
}

// describe returns short human-readable lines for the text and tool calls of an assistant event.
func describe(event streamEvent) []string {
	if event.Type != "assistant" {
		return nil
	}
	var lines []string
	for _, block := range event.Message.Content {
		switch block.Type {
		case "tool_use":
			lines = append(lines, strings.TrimSpace(fmt.Sprintf("`%s` %s", block.Name, short(target(block.Input), 120))))
		case "text":
			if text := short(block.Text, 120); text != "" {
				lines = append(lines, text)
			}
		}
	}
	return lines
}

func target(input map[string]any) string {
	for _, key := range []string{"command", "file_path", "pattern", "description"} {
		if value, ok := input[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// short collapses whitespace and clips to limit runes.
func short(text string, limit int) string {
	return clip(strings.Join(strings.Fields(text), " "), limit)
}

func clip(text string, limit int) string {
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}

func progress(role string, number int, steps []string) string {
	shown := "- starting…"
	if len(steps) > 0 {
		shown = "- " + strings.Join(steps[max(0, len(steps)-maxSteps):], "\n- ")
	}
	return fmt.Sprintf("🤖 Agent `%s` working on #%d (%d steps)\n\n%s", role, number, len(steps), shown)
}

func summary(role string, steps []string, result *streamEvent, exitCode int, stderr string) string {
	status := "✅ finished"
	switch {
	case exitCode == 124: // timeout(1) in agent.sh
		status = "❌ timed out after 45m"
	case exitCode != 0:
		status = fmt.Sprintf("❌ failed (exit code %d)", exitCode)
	}
	if result != nil && result.Subtype != "" && result.Subtype != "success" {
		status += fmt.Sprintf(" · Claude Code: `%s`", result.Subtype)
	}
	stats, text := "turns: ? · cost: unknown, not counted in the spec budget", "(no result message from Claude Code)"
	if result != nil {
		stats = fmt.Sprintf("turns: %d · cost: $%.2f", result.NumTurns, result.TotalCostUSD)
		text = clip(result.Result, 3000)
	}
	out := fmt.Sprintf("🤖 Agent `%s`: %s\n\nSteps: %d · %s\n\n%s", role, status, len(steps), stats, text)
	if result != nil {
		// Machine-readable copy for the per-spec budget, which must survive orchestrator restarts.
		out += fmt.Sprintf("\n\n<!-- forge:cost=%.4f -->", result.TotalCostUSD)
	}
	if exitCode != 0 && stderr != "" {
		out += "\n\nLast agent log lines:\n```\n" + stderr + "\n```"
	}
	return out
}

// runAgent runs one agent role on an issue, mirroring its stream into a single edited comment.
// feedback, when set, tells the agent why its previous attempt was rejected.
// It reports whether the agent succeeded (exit code 0) and returns its final message.
// A run stopped by the Claude usage limit is not the agent's fault: it waits for the reset and runs again,
// so the limit never uses up a story's attempts.
func (o *project) runAgent(role string, is issue, feedback string) (bool, string, error) {
	key := fmt.Sprintf("%s#%d", o.name, is.Number)
	defer o.setActivity(key, nil)
	for {
		ok, final, err := o.runOnce(role, is, feedback)
		reset, limited := limitReset(final, time.Now())
		if ok || err != nil || !limited {
			return ok, final, err
		}
		msg := fmt.Sprintf("⏸️ Claude usage limit reached: the `%s` runs again after %s.", role, reset.UTC().Format("2006-01-02 15:04 MST"))
		if _, err := o.gitea.comment(is.Number, msg); err != nil {
			return false, "", err
		}
		o.setActivity(key, &agentActivity{Role: role, Started: time.Now(), Waiting: "Claude usage limit: runs again after " + reset.UTC().Format("15:04 MST")})
		time.Sleep(time.Until(reset))
	}
}

func (o *project) runOnce(role string, is issue, feedback string) (bool, string, error) {
	key, err := os.ReadFile(o.cfg.DeployKeyFile)
	if err != nil {
		return false, "", err
	}
	commentID, err := o.gitea.comment(is.Number, fmt.Sprintf("🤖 Agent `%s` picked up #%d", role, is.Number))
	if err != nil {
		return false, "", err
	}
	env := append([]string{
		"ROLE=" + role,
		"REPO_URL=" + o.sshURL,
		"DEPLOY_KEY=" + string(key),
		"ISSUE_NUMBER=" + strconv.Itoa(is.Number),
		"ISSUE_TITLE=" + is.Title,
		"ISSUE_BODY=" + is.Body,
		"FEEDBACK=" + feedback,
		"PROTECTED_PATHS=" + strings.Join(o.cfg.Protected, ","), // for the guard hook (agents/guard.mjs)
	}, o.cfg.ClaudeEnv...)
	id, err := o.docker.run(o.cfg.AgentImage, o.cfg.AgentNetwork, env)
	if id != "" {
		defer func() {
			if err := o.docker.remove(id); err != nil {
				log.Printf("remove container %s: %v", id, err)
			}
		}()
	}
	if err != nil {
		return false, "", err
	}

	// Stdout carries only Claude's stream-json, one event per line.
	stdout, err := o.docker.logs(id, "follow=1&stdout=1")
	if err != nil {
		return false, "", err
	}
	defer stdout.Close()
	var steps []string
	var result *streamEvent
	var lastEdit time.Time
	activityKey := fmt.Sprintf("%s#%d", o.name, is.Number)
	o.setActivity(activityKey, &agentActivity{Role: role, Started: time.Now()})
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024) // tool results can embed whole files in one line
	for scanner.Scan() {
		event := parse(scanner.Bytes())
		if event.Type == "result" {
			result = &event
		}
		if lines := describe(event); len(lines) > 0 {
			steps = append(steps, lines...)
			o.setSteps(activityKey, steps)
		}
		if time.Since(lastEdit) > editEvery {
			// The progress comment is cosmetic: a failed edit must not kill the run.
			if err := o.gitea.editComment(commentID, progress(role, is.Number, steps)); err != nil {
				log.Printf("%s: progress comment on #%d: %v", o.name, is.Number, err)
			}
			lastEdit = time.Now()
		}
	}
	if err := scanner.Err(); err != nil {
		return false, "", err
	}

	exitCode, err := o.docker.wait(id)
	if err != nil {
		return false, "", err
	}
	if err := o.gitea.editComment(commentID, summary(role, steps, result, exitCode, o.docker.stderrTail(id, 20))); err != nil {
		return false, "", err
	}
	final := ""
	if result != nil {
		final = result.Result
	}
	return exitCode == 0, handoff(final, exitCode, steps), nil
}

// handoff is what a run leaves the next one: its final message or, for a run cut short without one, its last steps.
func handoff(final string, exitCode int, steps []string) string {
	if strings.TrimSpace(final) != "" || exitCode == 0 || len(steps) == 0 {
		return final
	}
	return "(cut short, no final message) Its last steps were:\n- " + strings.Join(steps[max(0, len(steps)-maxSteps):], "\n- ")
}

// agentActivity is what an issue's agent does now, as the UI shows it.
type agentActivity struct {
	Role    string    `json:"role"`
	Started time.Time `json:"started"`
	Steps   []string  `json:"steps"`             // the last maxSteps
	Total   int       `json:"total"`             // steps so far
	Waiting string    `json:"waiting,omitempty"` // why it is paused, if it is
}

// setActivity records an issue's agent activity; nil clears it.
func (f *forge) setActivity(key string, a *agentActivity) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a == nil {
		delete(f.activity, key)
	} else {
		f.activity[key] = a
	}
}

func (f *forge) setSteps(key string, steps []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a := f.activity[key]; a != nil {
		a.Steps = slices.Clone(steps[max(0, len(steps)-maxSteps):])
		a.Total = len(steps)
	}
}
