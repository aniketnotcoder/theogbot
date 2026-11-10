package links

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

var ErrUnsupportedLink = errors.New("unsupported Telegram message link")

type MessageLink struct {
	Username  string
	ChannelID int64
	MessageID int
}

// Parse accepts message permalinks in t.me, telegram.me and tg://privatepost
// form. Private /c/ links only identify a channel; the account must already
// have access to that channel. Invite links are deliberately not treated as
// message links because they do not identify a post.
func Parse(raw string) (MessageLink, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return MessageLink{}, ErrUnsupportedLink
	}
	u, err := url.Parse(raw)
	if err != nil {
		return MessageLink{}, fmt.Errorf("%w: %v", ErrUnsupportedLink, err)
	}
	if u.Scheme == "tg" && u.Host == "privatepost" {
		channel, e1 := strconv.ParseInt(u.Query().Get("channel"), 10, 64)
		post, e2 := strconv.Atoi(u.Query().Get("post"))
		if e1 != nil || e2 != nil || channel == 0 || post <= 0 {
			return MessageLink{}, ErrUnsupportedLink
		}
		return MessageLink{ChannelID: channel, MessageID: post}, nil
	}
	host := strings.ToLower(u.Hostname())
	if host != "t.me" && host != "telegram.me" && host != "www.t.me" && host != "www.telegram.me" {
		return MessageLink{}, ErrUnsupportedLink
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return MessageLink{}, ErrUnsupportedLink
	}
	// Public links: https://t.me/<username>/<message_id> and /s/<username>/<id>.
	if parts[0] == "s" {
		parts = parts[1:]
	}
	if len(parts) == 2 {
		id, err := strconv.Atoi(parts[1])
		if err != nil || id <= 0 || !validUsername(parts[0]) {
			return MessageLink{}, ErrUnsupportedLink
		}
		return MessageLink{Username: parts[0], MessageID: id}, nil
	}
	// Forum message permalinks include the topic ID before the actual post ID:
	// public: https://t.me/<username>/<topic_id>/<message_id>
	// private: https://t.me/c/<internal_channel_id>/<topic_id>/<message_id>.
	if len(parts) == 3 && parts[0] != "c" {
		id, err := strconv.Atoi(parts[2])
		topicID, topicErr := strconv.Atoi(parts[1])
		if err == nil && topicErr == nil && id > 0 && topicID > 0 && validUsername(parts[0]) {
			return MessageLink{Username: parts[0], MessageID: id}, nil
		}
	}
	if len(parts) == 4 && parts[0] == "c" {
		channel, channelErr := strconv.ParseInt(parts[1], 10, 64)
		topicID, topicErr := strconv.Atoi(parts[2])
		id, idErr := strconv.Atoi(parts[3])
		if channelErr == nil && topicErr == nil && idErr == nil && channel > 0 && topicID > 0 && id > 0 {
			return MessageLink{ChannelID: channel, MessageID: id}, nil
		}
	}
	// Private channel post links: https://t.me/c/<internal_channel_id>/<message_id>.
	if len(parts) == 3 && parts[0] == "c" {
		channel, e1 := strconv.ParseInt(parts[1], 10, 64)
		id, e2 := strconv.Atoi(parts[2])
		if e1 != nil || e2 != nil || channel <= 0 || id <= 0 {
			return MessageLink{}, ErrUnsupportedLink
		}
		return MessageLink{ChannelID: channel, MessageID: id}, nil
	}
	return MessageLink{}, ErrUnsupportedLink
}

func validUsername(s string) bool {
	if len(s) < 5 || len(s) > 32 {
		return false
	}
	for i, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
		if i == 0 && r >= '0' && r <= '9' {
			return false
		}
	}
	return true
}
