package deploy

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

var (
	stageOrder = []string{"package", "reserve", "upload", "finalize", "validate", "publish"}
	startLabel = map[string]string{
		"package":  "Packaging source bundle",
		"reserve":  "Reserving source snapshot",
		"upload":   "Uploading bundle",
		"finalize": "Finalizing snapshot",
		"validate": "Validating manifest",
		"publish":  "Publishing",
	}
	doneLabel = map[string]string{
		"package":  "Packaged source bundle",
		"reserve":  "Reserved source snapshot",
		"upload":   "Uploaded bundle",
		"finalize": "Finalized snapshot",
		"validate": "Validated manifest",
		"publish":  "Published",
	}
)

// ConsoleProgress renders stage events to an output stream (typically stderr).
// When the stream is a TTY it uses \r\x1b[2K to rewrite the in-progress line;
// otherwise it only emits a single finalized line per stage.
type ConsoleProgress struct {
	w          io.Writer
	isTTY      bool
	total      int
	stageIndex map[string]int
	started    map[string]time.Time
}

// NewConsoleProgress constructs a renderer targeting w. Pass os.Stderr for
// normal CLI use.
func NewConsoleProgress(w io.Writer) *ConsoleProgress {
	idx := make(map[string]int, len(stageOrder))
	for i, name := range stageOrder {
		idx[name] = i + 1
	}
	return &ConsoleProgress{
		w:          w,
		isTTY:      isTerminal(w),
		total:      len(stageOrder),
		stageIndex: idx,
		started:    map[string]time.Time{},
	}
}

// Handle is suitable as a deploy.Config.Progress callback.
func (c *ConsoleProgress) Handle(event string, data map[string]any) {
	stage, phase := splitEvent(event)
	step, ok := c.stageIndex[stage]
	if !ok {
		return
	}
	switch phase {
	case "start":
		c.started[stage] = time.Now()
		if c.isTTY {
			c.writeInline(c.formatLine(step, startLabel[stage], c.startDetail(stage, data), "...", 0, false))
		}
	case "complete":
		var elapsed time.Duration
		if t, ok := c.started[stage]; ok {
			elapsed = time.Since(t)
		}
		c.finalize(c.formatLine(step, doneLabel[stage], c.completeDetail(stage, data), "", elapsed, true))
	}
}

func (c *ConsoleProgress) startDetail(stage string, data map[string]any) string {
	if stage == "upload" {
		if n, ok := intFromAny(data["size_bytes"]); ok {
			return "(" + formatBytes(n) + ")"
		}
	}
	return ""
}

func (c *ConsoleProgress) completeDetail(stage string, data map[string]any) string {
	switch stage {
	case "package":
		var bits []string
		if n, ok := intFromAny(data["file_count"]); ok {
			noun := "files"
			if n == 1 {
				noun = "file"
			}
			bits = append(bits, fmt.Sprintf("%d %s", n, noun))
		}
		if n, ok := intFromAny(data["size_bytes"]); ok {
			bits = append(bits, formatBytes(n))
		}
		if len(bits) > 0 {
			return "(" + strings.Join(bits, ", ") + ")"
		}
	case "publish":
		if s, ok := data["agent_id"].(string); ok && strings.TrimSpace(s) != "" {
			return "(agent_id=" + s + ")"
		}
	}
	return ""
}

func (c *ConsoleProgress) formatLine(step int, label, detail, suffix string, elapsed time.Duration, showElapsed bool) string {
	parts := []string{fmt.Sprintf("[%d/%d] %s", step, c.total, label)}
	if detail != "" {
		parts = append(parts, detail)
	}
	if showElapsed && elapsed > 0 {
		parts = append(parts, fmt.Sprintf("(%.1fs)", elapsed.Seconds()))
	}
	line := strings.Join(parts, " ")
	if suffix != "" {
		line += suffix
	}
	return line
}

func (c *ConsoleProgress) writeInline(line string) {
	_, _ = fmt.Fprint(c.w, "\r\x1b[2K"+line)
	c.flush()
}

func (c *ConsoleProgress) finalize(line string) {
	prefix := ""
	if c.isTTY {
		prefix = "\r\x1b[2K"
	}
	_, _ = fmt.Fprint(c.w, prefix+line+"\n")
	c.flush()
}

func (c *ConsoleProgress) flush() {
	if f, ok := c.w.(*os.File); ok {
		_ = f.Sync()
	}
}

func splitEvent(event string) (string, string) {
	if i := strings.IndexByte(event, ':'); i >= 0 {
		return event[:i], event[i+1:]
	}
	return event, ""
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func formatBytes(size int) string {
	value := float64(size)
	units := []string{"B", "KB", "MB", "GB"}
	for i, unit := range units {
		if abs(value) < 1024 || i == len(units)-1 {
			if unit == "B" {
				return fmt.Sprintf("%d %s", int(value), unit)
			}
			return fmt.Sprintf("%.1f %s", value, unit)
		}
		value /= 1024
	}
	return fmt.Sprintf("%.1f GB", value)
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
