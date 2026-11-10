package forward
import("context";"errors";"fmt";"sort";"strconv";"strings";"time";"github.com/gotd/td/tg")
var ErrNotForum=errors.New("source chat is not a Telegram forum")
type ForumTopic struct{ID int;Title string;Date int;IDs []int}
func FetchForumTopics(ctx context.Context,api *tg.Client,peer tg.InputPeerClass,maxMessages int)([]ForumTopic,error){
 if api==nil||peer==nil{return nil,errors.New("MTProto API and source peer are required")}
 cp,ok:=peer.(*tg.InputPeerChannel);if !ok{return nil,ErrNotForum}
 if maxMessages<=0{maxMessages=5000};if maxMessages>50000{return nil,errors.New("forum copy limit cannot exceed 50000 messages")}
 resolved,err:=api.ChannelsGetChannels(ctx,[]tg.InputChannelClass{&tg.InputChannel{ChannelID:cp.ChannelID,AccessHash:cp.AccessHash}});if err!=nil{return nil,fmt.Errorf("inspect source forum: %w",err)}
 var channel *tg.Channel
 switch v:=resolved.(type){case *tg.MessagesChats:for _,x:=range v.Chats{if c,ok:=x.(*tg.Channel);ok&&c.ID==cp.ChannelID{channel=c;break}};case *tg.MessagesChatsSlice:for _,x:=range v.Chats{if c,ok:=x.(*tg.Channel);ok&&c.ID==cp.ChannelID{channel=c;break}}}
 if channel==nil{return nil,fmt.Errorf("Telegram did not return source channel %d",cp.ChannelID)};if !channel.Forum{return nil,ErrNotForum}
 topics,err:=fetchForumTopicList(ctx,api,peer);if err!=nil{return nil,err};sortForumTopics(topics)
 remaining:=maxMessages
 for i:=range topics{if remaining<=0{break};ids,e:=fetchForumTopicIDs(ctx,api,peer,topics[i].ID,remaining);if e!=nil{return nil,fmt.Errorf("read forum topic %q (%d): %w",topics[i].Title,topics[i].ID,e)};topics[i].IDs=ids;remaining-=len(ids)}
 return topics,nil
}
func fetchForumTopicList(ctx context.Context,api *tg.Client,peer tg.InputPeerClass)([]ForumTopic,error){
 const size=100;byID:=map[int]ForumTopic{};offID,offTopic,offDate:=0,0,0
 for n:=0;n<10000;n++{q,cancel:=context.WithTimeout(ctx,20*time.Second);page,e:=api.MessagesGetForumTopics(q,&tg.MessagesGetForumTopicsRequest{Peer:peer,OffsetID:offID,OffsetTopic:offTopic,OffsetDate:offDate,Limit:size});cancel();if e!=nil{return nil,fmt.Errorf("get forum topics page %d: %w",n+1,e)};if len(page.Topics)==0{break}
  var last *tg.ForumTopic;dates:=map[int]int{};for _,x:=range page.Messages{if m,ok:=x.(*tg.Message);ok{dates[m.ID]=m.Date}}
  for _,x:=range page.Topics{t,ok:=x.(*tg.ForumTopic);if !ok||t.ID<=0{continue};byID[t.ID]=ForumTopic{ID:t.ID,Title:strings.TrimSpace(t.Title),Date:t.Date};last=t}
  if last==nil||(last.ID==offTopic&&last.TopMessage==offID){break};offTopic,offID,offDate=last.ID,last.TopMessage,last.Date;if d,ok:=dates[last.TopMessage];ok&&!page.OrderByCreateDate{offDate=d}
  if page.Count>0&&len(byID)>=page.Count{break};if len(page.Topics)<size&&(page.Count==0||len(byID)>=page.Count){break}
 }
 out:=make([]ForumTopic,0,len(byID));for _,t:=range byID{if t.Title==""{if t.ID==1{t.Title="General"}else{t.Title="Topic "+strconv.Itoa(t.ID)}};out=append(out,t)};return out,nil
}
func fetchForumTopicIDs(ctx context.Context,api *tg.Client,peer tg.InputPeerClass,topicID,maxMessages int)([]int,error){
 const size=100;ids:=[]int{};seen:=map[int]struct{}{};offID:=0
 for n:=0;n<10000&&len(ids)<maxMessages;n++{q,cancel:=context.WithTimeout(ctx,20*time.Second);res,e:=api.MessagesGetReplies(q,&tg.MessagesGetRepliesRequest{Peer:peer,MsgID:topicID,OffsetID:offID,Limit:size});cancel();if e!=nil{return nil,e};pageIDs,oldest,count,e:=forumReplyPage(res);if e!=nil{return nil,e};for _,id:=range pageIDs{if _,ok:=seen[id];!ok{seen[id]=struct{}{};ids=append(ids,id)}};if count==0||oldest<=0||oldest==offID||count<size{break};offID=oldest}
 sort.Ints(ids);if len(ids)>maxMessages{ids=append([]int(nil),ids[len(ids)-maxMessages:]...)};return ids,nil
}
func forumReplyPage(res tg.MessagesMessagesClass)([]int,int,int,error){var items []tg.MessageClass;switch v:=res.(type){case *tg.MessagesMessages:items=v.Messages;case *tg.MessagesMessagesSlice:items=v.Messages;case *tg.MessagesChannelMessages:items=v.Messages;default:return nil,0,0,fmt.Errorf("unsupported forum replies response %T",res)};ids:=make([]int,0,len(items));oldest:=0;for _,x:=range items{if x==nil{continue};id:=x.GetID();if id>0&&(oldest==0||id<oldest){oldest=id};if _,ok:=x.(*tg.Message);ok&&id>0{ids=append(ids,id)}};return ids,oldest,len(items),nil}
func sortForumTopics(ts []ForumTopic){sort.SliceStable(ts,func(i,j int)bool{a,b:=ts[i],ts[j];if a.ID==1||b.ID==1{return a.ID==1&&b.ID!=1};an,aok:=topicNumber(a.Title);bn,bok:=topicNumber(b.Title);if aok&&bok&&an!=bn{return an<bn};if aok!=bok{return aok};if a.Date!=b.Date{return a.Date<b.Date};return a.ID<b.ID})}
func topicNumber(s string)(int,bool){s=strings.TrimSpace(s);i:=0;for i<len(s)&&s[i]>='0'&&s[i]<='9'{i++};if i==0||i==len(s){return 0,false};c:=s[i];if c!='.'&&c!=')'&&c!='-'&&c!=':'&&c!=' '{return 0,false};n,e:=strconv.Atoi(s[:i]);return n,e==nil}
