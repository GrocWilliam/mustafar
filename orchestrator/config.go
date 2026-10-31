package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// config holds the settings read from the environment (.env via docker compose).
type config struct {
	GiteaURL, GiteaToken string
	// SSHBase prefixes `owner/repo.git` for agents to clone and push inside the compose network (Gitea's sshd listens on 22 there).
	SSHBase                                 string
	AgentImage, AgentNetwork, DeployKeyFile string
	PollEvery                               time.Duration
	// MaxAgents caps the specs and US worked on at once, each running one agent at a time.
	MaxAgents int
	// Protected lists path prefixes a pull request must not touch (the CI that judges the agent, among others).
	Protected []string
	// MaxCostPerSpec stops a spec's remaining stories once its agents have cost more (USD, as reported by Claude Code).
	MaxCostPerSpec float64
	// ClaudeEnv forwards the API key or the subscription OAuth token, whichever is set, to agents.
	ClaudeEnv []string
}

func loadConfig() (config, error) {
	c := config{
		GiteaURL:      env("GITEA_URL", "http://gitea:3000"),
		GiteaToken:    os.Getenv("GITEA_TOKEN"),
		SSHBase:       env("GITEA_SSH_URL", "ssh://git@gitea:22/"),
		AgentImage:    env("AGENT_IMAGE", "forge-agent"),
		AgentNetwork:  env("AGENT_NETWORK", "forge"),
		DeployKeyFile: env("DEPLOY_KEY_FILE", "/run/secrets/deploy_key"),
	}
	if c.GiteaToken == "" {
		return c, errors.New("GITEA_TOKEN is required")
	}
	seconds, err := strconv.Atoi(env("POLL_SECONDS", "30"))
	if err != nil {
		return c, fmt.Errorf("POLL_SECONDS: %w", err)
	}
	c.PollEvery = time.Duration(seconds) * time.Second
	if c.MaxAgents, err = strconv.Atoi(env("MAX_AGENTS", "1")); err != nil || c.MaxAgents < 1 {
		return c, fmt.Errorf("MAX_AGENTS must be a positive integer")
	}
	c.Protected = strings.Split(env("PROTECTED_PATHS", ".gitea/"), ",")
	if c.MaxCostPerSpec, err = strconv.ParseFloat(env("MAX_COST_PER_SPEC", "20"), 64); err != nil {
		return c, fmt.Errorf("MAX_COST_PER_SPEC: %w", err)
	}
	for _, key := range []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if value := os.Getenv(key); value != "" {
			c.ClaudeEnv = append(c.ClaudeEnv, key+"="+value)
		}
	}
	return c, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
