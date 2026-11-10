package bot

import (
	"fmt"
	"strconv"
	"strings"
)

const maxBatchRange = 50000

// parseBatchCommand accepts the legacy list of post links plus either
// "/batch <chat-link> <start> to <end>" or "/batch <chat-link> <start>-<end>".
// It expands an inclusive, ascending message-ID range into exact post links.
func parseBatchCommand(fields []string) ([]string, error) {
	if len(fields) < 2 {
		return nil, fmt.Errorf("Usage: /batch <post-link> [<post-link> ...] or /batch <chat-link> <start> to <end>")
	}
	var startText, endText string
	var base string
	switch {
	case len(fields) == 5 && strings.EqualFold(fields[3], "to"):
		base, startText, endText = fields[1], fields[2], fields[4]
	case len(fields) == 3 && strings.Contains(fields[2], "-"):
		base = fields[1]
		parts := strings.Split(fields[2], "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("Invalid range. Use /batch <chat-link> 61 to 309.")
		}
		startText, endText = parts[0], parts[1]
	default:
		return append([]string(nil), fields[1:]...), nil
	}

	start, err1 := strconv.Atoi(startText)
	end, err2 := strconv.Atoi(endText)
	if err1 != nil || err2 != nil || start <= 0 || end < start {
		return nil, fmt.Errorf("Invalid message-ID range. Use positive ascending IDs, for example: /batch <chat-link> 61 to 309.")
	}
	count := end - start + 1
	if count > maxBatchRange {
		return nil, fmt.Errorf("A batch range can contain at most %d messages.", maxBatchRange)
	}
	username, channelID, err := parseChatLink(base)
	if err != nil {
		return nil, fmt.Errorf("Invalid source chat link: %w", err)
	}
	canonical := ""
	if username != "" {
		canonical = "https://t.me/" + username
	} else {
		canonical = fmt.Sprintf("https://t.me/c/%d", channelID)
	}
	result := make([]string, 0, count)
	for id := start; id <= end; id++ {
		result = append(result, fmt.Sprintf("%s/%d", canonical, id))
	}
	return result, nil
}
