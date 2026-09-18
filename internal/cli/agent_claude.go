package cli

func configureClaudeCommand(command *agentCommand, key, routerID string) {
	command.env = withEnvironment(command.env, map[string]string{
		"ANTHROPIC_BASE_URL":             agentRoutingBaseURL(routerID),
		"ANTHROPIC_AUTH_TOKEN":           key,
		"ANTHROPIC_API_KEY":              "",
		"CLAUDE_CODE_EFFORT_LEVEL":       "auto",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "1000000",
		"CLAUDE_CODE_SUBAGENT_MODEL":     routingModel,
	})
	command.args = insertBeforeDoubleDash(command.args, []string{"--model", routingModel})
}
