package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// config holds the settings read from the environment (.env via docker compose).
type config struct {
	GiteaURL, GiteaToken string
	// SSHBase prefixes `owner/repo.git` for agents to clone and push inside the compose network (Gitea's sshd listens on 22 there).
	SSHBase                                 string
	AgentImage, AgentNetwork, DeployKeyFile string
	// PublishIP is the host address staging apps listen on: 127.0.0.1, or 0.0.0.0 for the LAN.
	PublishIP string
	// DeployKeyPubFile and CITemplate are what a project created from the UI gets: the agents' key, the CI workflow.
	DeployKeyPubFile, CITemplate, MemoryTemplate string
	// Defaults are the .env settings; SettingsFile holds the UI's changes, which win (settings.go).
	Defaults     settings
	SettingsFile string
	// Protected lists path prefixes a pull request must not touch (the CI that judges the agent, among others).
	Protected []string
	// ClaudeEnv forwards the API key or the subscription OAuth token, whichever is set, to agents.
	ClaudeEnv []string
}

func loadConfig() (config, error) {
	c := config{
		GiteaURL:         env("GITEA_URL", "http://gitea:3000"),
		GiteaToken:       os.Getenv("GITEA_TOKEN"),
		SSHBase:          env("GITEA_SSH_URL", "ssh://git@gitea:22/"),
		AgentImage:       env("AGENT_IMAGE", "forge-agent"),
		AgentNetwork:     env("AGENT_NETWORK", "forge"),
		PublishIP:        env("PUBLISH_IP", "127.0.0.1"),
		DeployKeyFile:    env("DEPLOY_KEY_FILE", "/run/secrets/deploy_key"),
		DeployKeyPubFile: env("DEPLOY_KEY_PUB_FILE", "/run/secrets/deploy_key.pub"),
		CITemplate:       env("CI_TEMPLATE", "/templates/ci.yml"),
		MemoryTemplate:   env("MEMORY_TEMPLATE", "/templates/CLAUDE.md"),
		SettingsFile:     env("SETTINGS_FILE", "/data/settings.json"),
	}
	if c.GiteaToken == "" {
		return c, errors.New("GITEA_TOKEN is required")
	}
	var err error
	if c.Defaults.PollSeconds, err = strconv.Atoi(env("POLL_SECONDS", "30")); err != nil {
		return c, fmt.Errorf("POLL_SECONDS: %w", err)
	}
	if c.Defaults.MaxAgents, err = strconv.Atoi(env("MAX_AGENTS", "1")); err != nil {
		return c, fmt.Errorf("MAX_AGENTS: %w", err)
	}
	for _, prefix := range strings.Split(env("PROTECTED_PATHS", ".gitea/"), ",") {
		// An untrimmed " Makefile" would match nothing, an empty prefix every file.
		if prefix = strings.TrimSpace(prefix); prefix != "" {
			c.Protected = append(c.Protected, prefix)
		}
	}
	if c.Defaults.MaxCostPerSpec, err = strconv.ParseFloat(env("MAX_COST_PER_SPEC", "20"), 64); err != nil {
		return c, fmt.Errorf("MAX_COST_PER_SPEC: %w", err)
	}
	if err := c.Defaults.validate(); err != nil {
		return c, fmt.Errorf(".env: %w", err)
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
