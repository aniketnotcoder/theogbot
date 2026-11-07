package forward

import (
 "context"
 "errors"
 "fmt"
 "github.com/gotd/td/telegram/message/peer"
 "github.com/gotd/td/telegram/query/messages"
 "github.com/gotd/td/tg"
)

const MaxExactMessagesPerRequest = 100

// FetchMessagesByID retrieves only explicitly requested message IDs and returns
// them in caller order. It never scans chat history.
func FetchMessagesByID(ctx context.Context, api *tg.Client, source tg.InputPeerClass, ids []int) ([]messages.Elem, error) {
 if api == nil || source == nil { return nil, errors.New("MTProto API and source peer are required") }
 if len(ids) == 0 { return nil, errors.New("no message IDs supplied") }
 for _, id := range ids { if id <= 0 { return nil, fmt.Errorf("invalid message ID %d", id) } }
 found := make(map[int]messages.Elem, len(ids))
 for start := 0; start < len(ids); start += MaxExactMessagesPerRequest {
  end := start + MaxExactMessagesPerRequest
  if end > len(ids) { end = len(ids) }
  batch := ids[start:end]
  inputIDs := make([]tg.InputMessageClass, 0, len(batch))
  for _, id := range batch { inputIDs = append(inputIDs, &tg.InputMessageID{ID:id}) }
  var result tg.MessagesMessagesClass
  var err error
  switch p := source.(type) {
  case *tg.InputPeerChannel:
   result, err = api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel:&tg.InputChannel{ChannelID:p.ChannelID, AccessHash:p.AccessHash}, ID:inputIDs})
  default:
   result, err = api.MessagesGetMessages(ctx, inputIDs)
  }
  if err != nil { return nil, fmt.Errorf("retrieve requested messages %d-%d: %w", batch[0], batch[len(batch)-1], err) }
  switch value := result.(type) {
  case *tg.MessagesMessages:
   entities := peer.EntitiesFromResult(value)
   for _, item := range value.Messages { if msg, ok := item.AsNotEmpty(); ok { found[msg.GetID()] = messages.Elem{Msg:msg, Peer:source, Entities:entities} } }
  case *tg.MessagesMessagesSlice:
   entities := peer.EntitiesFromResult(value)
   for _, item := range value.Messages { if msg, ok := item.AsNotEmpty(); ok { found[msg.GetID()] = messages.Elem{Msg:msg, Peer:source, Entities:entities} } }
  case *tg.MessagesChannelMessages:
   entities := peer.EntitiesFromResult(value)
   for _, item := range value.Messages { if msg, ok := item.AsNotEmpty(); ok { found[msg.GetID()] = messages.Elem{Msg:msg, Peer:source, Entities:entities} } }
  default:
   return nil, fmt.Errorf("unsupported Telegram messages response %T", result)
  }
 }
 ordered := make([]messages.Elem, 0, len(ids))
 missing := make([]int, 0)
 for _, id := range ids { elem, ok := found[id]; if !ok { missing = append(missing, id); continue }; ordered = append(ordered, elem) }
 if len(missing) > 0 { return nil, fmt.Errorf("%d requested message(s) are unavailable: %v", len(missing), missing) }
 return ordered, nil
}
