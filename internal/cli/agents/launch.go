// Package agents holds what every coding-agent launcher shares: the command
// being built, argument and environment helpers, and the routing status each
// agent shows in its UI. Each agent's own setup lives in a subpackage.
package agents

import "strings"

const (
	RoutingAPIKeyEnv = "DARI_ROUTING_API_KEY"
	RoutingModel     = "dari/routing"
)

// Command is the agent process to start. Cleanup removes anything the
// launcher created for it, such as temporary settings or plugins.
type Command struct {
	Path    string
	Args    []string
	Env     []string
	Cleanup func()
}

// InsertBeforeDoubleDash adds arguments before any `--`, so they are read as
// options rather than passed through to a prompt.
func InsertBeforeDoubleDash(args, inserted []string) []string {
	at := len(args)
	for i, arg := range args {
		if arg == "--" {
			at = i
			break
		}
	}
	result := make([]string, 0, len(args)+len(inserted))
	result = append(result, args[:at]...)
	result = append(result, inserted...)
	result = append(result, args[at:]...)
	return result
}

func ArgsContainFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

// OnlyRequestsInfo reports whether a launch only asks for help or a version,
// which needs neither sign-in nor setup.
func OnlyRequestsInfo(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		switch arg {
		case "--help", "-h", "--version", "-V", "-v":
			return true
		}
	}
	return len(args) > 0 && args[0] == "help"
}

func WithEnvironment(base []string, values map[string]string) []string {
	result := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := values[name]; !replaced {
			result = append(result, entry)
		}
	}
	for name, value := range values {
		result = append(result, name+"="+value)
	}
	return result
}

func WithoutEnvironment(env []string, name string) []string {
	result := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, name+"=") {
			result = append(result, entry)
		}
	}
	return result
}

func EnvironmentValue(env []string, name string) string {
	prefix := name + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
