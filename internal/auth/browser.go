package auth

import (
	"os/exec"
	"runtime"
)

// openBrowser launches the user's default browser pointed at url. Returns
// true if the launch command started successfully; callers should still
// print the URL in case the user is headless or the launch silently failed.
func openBrowser(url string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start() == nil
}
