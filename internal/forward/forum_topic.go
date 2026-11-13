package forward

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotd/td/tg"
)

// ErrForumTopicRequired is returned when a forum resume link does not identify
// a specific topic.
var ErrForumTopicRequired = errors.New("forum resume requires a topic-specific Telegram post link")

// FetchForumTopic retrieves only the requested forum topic's messages. Unlike
// FetchForumTopics, it does not enumerate and fetch message histories for every
// topic in the forum.
func FetchForumTopic(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, topicID, maxMessages int) (ForumTopic, error) {
	if api == nil || peer == nil {
		return ForumTopic{}, errors.New("MTProto API and source peer are required")
	}
	cp, ok := peer.(*tg.InputPeerChannel)
	if !ok {
		return ForumTopic{}, ErrNotForum
	}
	resolved, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
		&tg.InputChannel{ChannelID: cp.ChannelID, AccessHash: cp.AccessHash},
	})
	if err != nil {
		return ForumTopic{}, fmt.Errorf("inspect source forum: %w", err)
	}
	var channel *tg.Channel
	switch v := resolved.(type) {
	case *tg.MessagesChats:
		for _, x := range v.Chats {
			if c, ok := x.(*tg.Channel); ok && c.ID == cp.ChannelID {
				channel = c
				break
			}
		}
	case *tg.MessagesChatsSlice:
		for _, x := range v.Chats {
			if c, ok := x.(*tg.Channel); ok && c.ID == cp.ChannelID {
				channel = c
				break
			}
		}
	}
	if channel == nil {
		return ForumTopic{}, fmt.Errorf("Telegram did not return source channel %d", cp.ChannelID)
	}
	if !channel.Forum {
		return ForumTopic{}, ErrNotForum
	}
	if topicID <= 0 {
		return ForumTopic{}, ErrForumTopicRequired
	}
	if maxMessages <= 0 {
		maxMessages = 5000
	}
	if maxMessages > 50000 {
		return ForumTopic{}, errors.New("forum topic resume limit cannot exceed 50000 messages")
	}
	topics, err := fetchForumTopicList(ctx, api, peer)
	if err != nil {
		return ForumTopic{}, err
	}
	for _, topic := range topics {
		if topic.ID != topicID {
			continue
		}
		ids, err := fetchForumTopicIDs(ctx, api, peer, topicID, maxMessages)
		if err != nil {
			return ForumTopic{}, fmt.Errorf("read forum topic %q (%d): %w", topic.Title, topic.ID, err)
		}
		topic.IDs = ids
		return topic, nil
	}
	return ForumTopic{}, fmt.Errorf("forum topic %d was not found or is not accessible", topicID)
}
