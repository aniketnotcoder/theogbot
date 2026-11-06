package forward

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/tg"
)

var ErrPeerNotFound = errors.New("Telegram peer was not returned by Telegram")

func ResolveUsername(ctx context.Context, api *tg.Client, username string) (tg.InputPeerClass, error) {
	if api == nil { return nil, errors.New("MTProto API is required") }
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if username == "" { return nil, errors.New("username is required") }
	resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if err != nil { return nil, fmt.Errorf("resolve @%s: %w", username, err) }
	switch peer := resolved.Peer.(type) {
	case *tg.PeerChannel:
		for _, item := range resolved.Chats {
			if ch, ok := item.(*tg.Channel); ok && ch.ID == peer.ChannelID {
				if ch.AccessHash == 0 { return nil, fmt.Errorf("%w: channel access hash unavailable", ErrPeerNotFound) }
				return &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
			}
		}
	case *tg.PeerChat:
		return &tg.InputPeerChat{ChatID: peer.ChatID}, nil
	case *tg.PeerUser:
		for _, item := range resolved.Users {
			if user, ok := item.(*tg.User); ok && user.ID == peer.UserID {
				if user.AccessHash == 0 { return nil, fmt.Errorf("%w: user access hash unavailable", ErrPeerNotFound) }
				return &tg.InputPeerUser{UserID: user.ID, AccessHash: user.AccessHash}, nil
			}
		}
	}
	return nil, ErrPeerNotFound
}

// FetchHistoryIDs reads accessible history in pages and returns IDs oldest-first.
// It never joins chats or attempts to bypass Telegram access/protection controls.
func FetchHistoryIDs(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, maxMessages int) ([]int, error) {
	if api == nil || peer == nil { return nil, errors.New("MTProto API and peer are required") }
	if maxMessages <= 0 { maxMessages = 5000 }
	if maxMessages > 50000 { return nil, errors.New("history limit cannot exceed 50000 messages") }
	ids := make([]int, 0)
	offsetID := 0
	for len(ids) < maxMessages {
		limit := 100
		if remaining := maxMessages-len(ids); remaining < limit { limit = remaining }
		requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		result, err := api.MessagesGetHistory(requestCtx, &tg.MessagesGetHistoryRequest{Peer: peer, OffsetID: offsetID, Limit: limit})
		cancel()
		if err != nil { return nil, fmt.Errorf("get chat history: %w", err) }
		page, err := extractMessageIDs(result)
		if err != nil { return nil, err }
		if len(page) == 0 { break }
		oldest := page[len(page)-1]
		if oldest <= 0 || oldest == offsetID { break }
		ids = append(ids, page...)
		offsetID = oldest
		if len(page) < limit { break }
	}
	seen := make(map[int]struct{}, len(ids))
	unique := ids[:0]
	for _, id := range ids { if id > 0 { if _, ok := seen[id]; !ok { seen[id]=struct{}{}; unique=append(unique,id) } } }
	for i,j:=0,len(unique)-1; i<j; i,j=i+1,j-1 { unique[i],unique[j]=unique[j],unique[i] }
	return unique,nil
}

func extractMessageIDs(result tg.MessagesMessagesClass) ([]int,error) {
	var messages []tg.MessageClass
	switch value:=result.(type) {
	case *tg.MessagesMessages: messages=value.Messages
	case *tg.MessagesMessagesSlice: messages=value.Messages
	case *tg.MessagesChannelMessages: messages=value.Messages
	default: return nil,fmt.Errorf("unsupported Telegram history response %T",result)
	}
	ids:=make([]int,0,len(messages))
	for _,item:=range messages {
		switch msg:=item.(type) {
		case *tg.Message: ids=append(ids,msg.ID)
		case *tg.MessageService: ids=append(ids,msg.ID)
		}
	}
	return ids,nil
}


// ResolvePrivateChannelID finds a channel already present in the authenticated
// account's dialogs. It does not join invite links or enumerate inaccessible chats.
func ResolvePrivateChannelID(ctx context.Context, api *tg.Client, channelID int64) (tg.InputPeerClass, error) {
	if api == nil || channelID <= 0 { return nil, errors.New("valid MTProto API and channel ID are required") }
	requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	result, err := api.MessagesGetDialogs(requestCtx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{}, Limit: 100, Hash: 0,
	})
	cancel()
	if err != nil { return nil, fmt.Errorf("read account dialogs: %w", err) }
	var chats []tg.ChatClass
	switch value := result.(type) {
	case *tg.MessagesDialogs: chats = value.Chats
	case *tg.MessagesDialogsSlice: chats = value.Chats
	default: return nil, fmt.Errorf("unsupported dialogs response %T", result)
	}
	for _, item := range chats {
		if ch, ok := item.(*tg.Channel); ok && ch.ID == channelID {
			if ch.AccessHash == 0 { return nil, fmt.Errorf("%w: channel access hash unavailable", ErrPeerNotFound) }
			return &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
		}
	}
	return nil, fmt.Errorf("%w: private channel is not in the first 100 account dialogs; use its public username if available", ErrPeerNotFound)
}
