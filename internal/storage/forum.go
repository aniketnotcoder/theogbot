package storage
import("context";"errors";"fmt";"os";"sort";"strings";"time";"github.com/gotd/td/crypto";"github.com/gotd/td/tg";"github.com/gotd/td/telegram/message/entity";"github.com/gotd/td/telegram/message/styling")
type ForumTopicInfo struct{ID int;Title string;Date int}
func(c *Client)IsForum(ctx context.Context,peer tg.InputPeerClass)(bool,error){cp,ok:=peer.(*tg.InputPeerChannel);if !ok{return false,nil};if e:=c.Ready(ctx);e!=nil{return false,e};c.mu.RLock();api:=c.api;c.mu.RUnlock();res,e:=api.ChannelsGetChannels(ctx,[]tg.InputChannelClass{&tg.InputChannel{ChannelID:cp.ChannelID,AccessHash:cp.AccessHash}});if e!=nil{return false,fmt.Errorf("inspect destination channel: %w",e)};switch v:=res.(type){case *tg.MessagesChats:for _,x:=range v.Chats{if ch,ok:=x.(*tg.Channel);ok&&ch.ID==cp.ChannelID{return ch.Forum,nil}};case *tg.MessagesChatsSlice:for _,x:=range v.Chats{if ch,ok:=x.(*tg.Channel);ok&&ch.ID==cp.ChannelID{return ch.Forum,nil}}};return false,fmt.Errorf("Telegram did not return destination channel %d",cp.ChannelID)}
func(c *Client)GetOrCreateForumTopic(ctx context.Context,peer tg.InputPeerClass,title string)(int,error){title=strings.TrimSpace(title);if title==""{return 0,errors.New("forum topic title is empty")};if len([]byte(title))>128{return 0,errors.New("forum topic title exceeds Telegram's 128-byte limit")};ok,e:=c.IsForum(ctx,peer);if e!=nil{return 0,e};if !ok{return 0,errors.New("destination is not a forum-enabled supergroup")};if strings.EqualFold(title,"General"){return 1,nil};c.mu.RLock();api:=c.api;c.mu.RUnlock();topics,e:=listDestinationForumTopics(ctx,api,peer);if e!=nil{return 0,e};if id:=findForumTopicID(topics,title);id>0{return id,nil};rid,e:=crypto.RandInt64(crypto.DefaultRand());if e!=nil{return 0,e};_,createErr:=api.MessagesCreateForumTopic(ctx,&tg.MessagesCreateForumTopicRequest{Peer:peer,Title:title,RandomID:rid,IconColor:0x6FB9F0});if createErr!=nil{topics,listErr:=listDestinationForumTopics(ctx,api,peer);if listErr==nil{if id:=findForumTopicID(topics,title);id>0{return id,nil}};return 0,fmt.Errorf("create destination forum topic %q: %w",title,createErr)};topics,e=listDestinationForumTopics(ctx,api,peer);if e!=nil{return 0,fmt.Errorf("verify created forum topic %q: %w",title,e)};if id:=findForumTopicID(topics,title);id>0{return id,nil};return 0,fmt.Errorf("Telegram created topic %q but did not return it in the topic list",title)}
func listDestinationForumTopics(ctx context.Context,api *tg.Client,peer tg.InputPeerClass)([]ForumTopicInfo,error){const size=100;byID:=map[int]ForumTopicInfo{};offID,offTopic,offDate:=0,0,0;for n:=0;n<10000;n++{q,cancel:=context.WithTimeout(ctx,20*time.Second);page,e:=api.MessagesGetForumTopics(q,&tg.MessagesGetForumTopicsRequest{Peer:peer,OffsetID:offID,OffsetTopic:offTopic,OffsetDate:offDate,Limit:size});cancel();if e!=nil{return nil,fmt.Errorf("list destination forum topics: %w",e)};if len(page.Topics)==0{break};var last *tg.ForumTopic;dates:=map[int]int{};for _,x:=range page.Messages{if m,ok:=x.(*tg.Message);ok{dates[m.ID]=m.Date}};for _,x:=range page.Topics{t,ok:=x.(*tg.ForumTopic);if !ok||t.ID<=0{continue};byID[t.ID]=ForumTopicInfo{ID:t.ID,Title:strings.TrimSpace(t.Title),Date:t.Date};last=t};if last==nil||(last.ID==offTopic&&last.TopMessage==offID){break};offTopic,offID,offDate=last.ID,last.TopMessage,last.Date;if d,ok:=dates[last.TopMessage];ok&&!page.OrderByCreateDate{offDate=d};if page.Count>0&&len(byID)>=page.Count{break};if len(page.Topics)<size&&(page.Count==0||len(byID)>=page.Count){break}};out:=make([]ForumTopicInfo,0,len(byID));for _,t:=range byID{out=append(out,t)};sort.Slice(out,func(i,j int)bool{return out[i].ID<out[j].ID});return out,nil}
func findForumTopicID(ts []ForumTopicInfo,title string)int{for _,t:=range ts{if strings.EqualFold(strings.TrimSpace(t.Title),strings.TrimSpace(title)){return t.ID}};return 0}
func forumReplyTo(id int)tg.InputReplyToClass{return &tg.InputReplyToMessage{ReplyToMsgID:id}}
func(c *Client)SendTextToForumTopic(ctx context.Context,dest tg.InputPeerClass,topicID int,text string)(tg.UpdatesClass,error){if topicID<=0||strings.TrimSpace(text)==""{return nil,errors.New("valid destination topic and non-empty text are required")};if e:=c.Ready(ctx);e!=nil{return nil,e};c.mu.RLock();api:=c.api;c.mu.RUnlock();rid,e:=crypto.RandInt64(crypto.DefaultRand());if e!=nil{return nil,e};return retryFloodWait(ctx,func()(tg.UpdatesClass,error){return api.MessagesSendMessage(ctx,&tg.MessagesSendMessageRequest{Peer:dest,ReplyTo:forumReplyTo(topicID),Message:text,RandomID:rid})})}
func(c *Client)UploadPathToForumTopicWithProgress(ctx context.Context,dest tg.InputPeerClass,topicID int,path,filename,mime,caption,kind string,attrs []tg.DocumentAttributeClass,entities []tg.MessageEntityClass,progress TransferProgress)(tg.UpdatesClass,error){if topicID<=0{return nil,errors.New("valid destination forum topic is required")};if e:=c.Ready(ctx);e!=nil{return nil,e};c.mu.RLock();api:=c.api;c.mu.RUnlock();info,e:=os.Stat(path);if e!=nil{return nil,fmt.Errorf("stat upload file: %w",e)};rid,e:=crypto.RandInt64(crypto.DefaultRand());if e!=nil{return nil,e};return retryFloodWait(ctx,func()(tg.UpdatesClass,error){file,e:=uploadFileWithRetry(ctx,api,path,info.Size(),progress);if e!=nil{return nil,fmt.Errorf("upload file: %w",e)};text,ents,e:=buildCaption(caption,entities);if e!=nil{return nil,e};var media tg.InputMediaClass;if kind=="photo"{media=&tg.InputMediaUploadedPhoto{File:file}}else{attrsOut:=make([]tg.DocumentAttributeClass,0,len(attrs)+1);for _,a:=range attrs{if _,ok:=a.(*tg.DocumentAttributeFilename);!ok{attrsOut=append(attrsOut,a)}};if filename!=""{attrsOut=append(attrsOut,&tg.DocumentAttributeFilename{FileName:filename})};if mime==""{mime="application/octet-stream"};media=&tg.InputMediaUploadedDocument{File:file,MimeType:mime,Attributes:attrsOut}};return api.MessagesSendMedia(ctx,&tg.MessagesSendMediaRequest{Peer:dest,ReplyTo:forumReplyTo(topicID),Media:media,Message:text,Entities:ents,RandomID:rid})})}
func(c *Client)UploadAlbumToForumTopicWithProgress(ctx context.Context,dest tg.InputPeerClass,topicID int,items []AlbumItem,progress TransferProgress)(tg.UpdatesClass,error){
 if topicID<=0{return nil,errors.New("valid destination forum topic is required")}
 if len(items)<2||len(items)>10{return nil,fmt.Errorf("album must contain between 2 and 10 items")}
 if e:=c.Ready(ctx);e!=nil{return nil,e}
 c.mu.RLock();api:=c.api;c.mu.RUnlock()
 var total int64
 randomIDs:=make([]int64,len(items))
 for i,item:=range items{
  if item.Kind!="photo"&&item.Kind!="video"{return nil,fmt.Errorf("album item %d has unsupported kind %q",i+1,item.Kind)}
  info,e:=os.Stat(item.Path);if e!=nil{return nil,fmt.Errorf("stat album item %d: %w",i+1,e)}
  total+=info.Size();randomIDs[i],e=crypto.RandInt64(crypto.DefaultRand());if e!=nil{return nil,e}
 }
 return retryFloodWait(ctx,func()(tg.UpdatesClass,error){
  mediaItems:=make([]tg.InputSingleMedia,0,len(items))
  for i,item:=range items{
   info,e:=os.Stat(item.Path);if e!=nil{return nil,fmt.Errorf("stat album item %d: %w",i+1,e)}
   file,e:=uploadFileWithRetry(ctx,api,item.Path,info.Size(),progress);if e!=nil{return nil,fmt.Errorf("upload album item %d: %w",i+1,e)}
   var uploaded tg.InputMediaClass
   if item.Kind=="photo"{uploaded=&tg.InputMediaUploadedPhoto{File:file}}else{
    attrs:=make([]tg.DocumentAttributeClass,0,len(item.Attributes)+1)
    for _,a:=range item.Attributes{if _,ok:=a.(*tg.DocumentAttributeFilename);!ok{attrs=append(attrs,a)}}
    if item.Filename!=""{attrs=append(attrs,&tg.DocumentAttributeFilename{FileName:item.Filename})}
    mime:=item.MIME;if mime==""{mime="application/octet-stream"}
    uploaded=&tg.InputMediaUploadedDocument{File:file,MimeType:mime,Attributes:attrs}
   }
   // Telegram requires messages.uploadMedia before uploaded media can be
   // included in messages.sendMultiMedia. Convert the returned media reference
   // to InputMediaPhoto/InputMediaDocument, matching gotd's Sender.Album path.
   uploadedResult,e:=api.MessagesUploadMedia(ctx,&tg.MessagesUploadMediaRequest{Peer:dest,Media:uploaded})
   if e!=nil{return nil,fmt.Errorf("prepare album item %d for destination: %w",i+1,e)}
   var inputMedia tg.InputMediaClass
   switch value:=uploadedResult.(type){
   case *tg.MessageMediaPhoto:
    photo,ok:=value.Photo.AsNotEmpty();if !ok{return nil,fmt.Errorf("album item %d returned an empty photo reference",i+1)}
    inputMedia=&tg.InputMediaPhoto{ID:photo.AsInput(),TTLSeconds:value.TTLSeconds}
   case *tg.MessageMediaDocument:
    doc,ok:=value.Document.AsNotEmpty();if !ok{return nil,fmt.Errorf("album item %d returned an empty document reference",i+1)}
    inputMedia=&tg.InputMediaDocument{ID:doc.AsInput(),TTLSeconds:value.TTLSeconds}
   default:
    return nil,fmt.Errorf("album item %d returned unsupported uploaded media %T",i+1,uploadedResult)
   }
   text,ents,e:=buildCaption(item.Caption,item.Entities);if e!=nil{return nil,e}
   mediaItems=append(mediaItems,tg.InputSingleMedia{Media:inputMedia,Message:text,Entities:ents,RandomID:randomIDs[i]})
  }
  return api.MessagesSendMultiMedia(ctx,&tg.MessagesSendMultiMediaRequest{Peer:dest,ReplyTo:forumReplyTo(topicID),MultiMedia:mediaItems})
 })
}
func buildCaption(caption string,entities []tg.MessageEntityClass)(string,[]tg.MessageEntityClass,error){if caption==""{return "",nil,nil};opts:=captionOptions(caption,entities);if len(opts)==0{return caption,nil,nil};builder:=entity.Builder{};if e:=styling.Perform(&builder,opts...);e!=nil{return "",nil,e};text,formatted:=builder.Complete();return text,formatted,nil}
