package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
)

type json3Doc struct {
	Events []json3Event `json:"events"`
}

type json3Event struct {
	StartMs int64      `json:"tStartMs"`
	Segs    []json3Seg `json:"segs"`
}

type json3Seg struct {
	UTF8 string `json:"utf8"`
}

// parseJSON3 flattens a json3 caption document into plain text, one line per
// caption event. With timestamps, each line is prefixed [m:ss] or [h:mm:ss].
func parseJSON3(data []byte, withTimestamps bool) (string, error) {
	var doc json3Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parsing caption data: %w", err)
	}
	var lines []string
	for _, ev := range doc.Events {
		var b strings.Builder
		for _, seg := range ev.Segs {
			b.WriteString(seg.UTF8)
		}
		text := strings.TrimSpace(strings.ReplaceAll(b.String(), "\n", " "))
		if text == "" {
			continue
		}
		if withTimestamps {
			text = fmt.Sprintf("[%s] %s", formatTimestamp(ev.StartMs), text)
		}
		lines = append(lines, text)
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("caption track was empty")
	}
	return strings.Join(lines, "\n"), nil
}

func formatTimestamp(ms int64) string {
	total := ms / 1000
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
