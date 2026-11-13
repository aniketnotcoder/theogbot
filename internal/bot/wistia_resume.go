package bot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aniketnotcoder/theogbot/internal/forward"
	"github.com/aniketnotcoder/theogbot/internal/links"
)

// parseWistiaResumeCommand accepts a permalink to the first message to retry,
// followed by an inclusive end message ID:
// /wistia resume https://t.me/c/<channel>/<topic>/<start> to <end> (forums).
func parseWistiaResumeCommand(fields []string) (string, int, int, error) {
	if len(fields) != 5 || !strings.EqualFold(fields[3], "to") {
		return "", 0, 0, fmt.Errorf("Usage: /wistia resume <failed-post-link> to <end-message-id>")
	}
	parsed, err := links.Parse(fields[2])
	if err != nil || parsed.MessageID <= 0 {
		return "", 0, 0, fmt.Errorf("provide a Telegram post link containing the first message ID to retry")
	}
	end, err := strconv.Atoi(fields[4])
	if err != nil || end < parsed.MessageID {
		return "", 0, 0, fmt.Errorf("end message ID must be greater than or equal to the linked message ID")
	}
	if end-parsed.MessageID+1 > maxBatchRange {
		return "", 0, 0, fmt.Errorf("resume range can contain at most %d message IDs", maxBatchRange)
	}
	return fields[2], parsed.MessageID, end, nil
}

// filterWistiaForumRange keeps only messages in the requested inclusive
// Telegram message-ID range while preserving the source forum's topic order.
func filterWistiaForumRange(topics []forward.ForumTopic, start, end int) []forward.ForumTopic {
	filtered := make([]forward.ForumTopic, 0, len(topics))
	for _, topic := range topics {
		ids := make([]int, 0, len(topic.IDs))
		for _, id := range topic.IDs {
			if id >= start && id <= end {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			continue
		}
		topic.IDs = ids
		filtered = append(filtered, topic)
	}
	return filtered
}


// filterWistiaForumTopicRange selects only the linked forum topic, then applies
// the inclusive Telegram message-ID range. Other topics are never returned.
func filterWistiaForumTopicRange(topics []forward.ForumTopic, topicID, start, end int) (forward.ForumTopic, bool) {
	for _, topic := range topics {
		if topic.ID != topicID {
			continue
		}
		ids := make([]int, 0, len(topic.IDs))
		for _, id := range topic.IDs {
			if id >= start && id <= end {
				ids = append(ids, id)
			}
		}
		topic.IDs = ids
		return topic, true
	}
	return forward.ForumTopic{}, false
}
