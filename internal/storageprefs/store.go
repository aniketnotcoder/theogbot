package storageprefs
import("encoding/json";"fmt";"os";"path/filepath";"strconv";"strings";"time";bolt "go.etcd.io/bbolt")
var bucketName=[]byte("user-storage-preferences")
var tutorialBucketName=[]byte("bot-settings")
type Destination struct{ChatID int64 `json:"chat_id"`;Title string `json:"title"`;Username string `json:"username,omitempty"`}
type Record struct{Groups []Destination `json:"groups"`;ActiveChatID int64 `json:"active_chat_id"`}
type Store struct{db *bolt.DB}
func Open(path string)(*Store,error){if path==""{return nil,fmt.Errorf("storage database path is empty")};dir:=filepath.Dir(path);if err:=os.MkdirAll(dir,0755);err!=nil{return nil,fmt.Errorf("create storage database directory %q: %w",dir,err)};db,err:=bolt.Open(path,0600,&bolt.Options{Timeout:1*time.Second});if err!=nil{return nil,fmt.Errorf("open storage database: %w",err)};if err:=db.Update(func(tx *bolt.Tx)error{if _,e:=tx.CreateBucketIfNotExists(bucketName);e!=nil{return e};_,e:=tx.CreateBucketIfNotExists(tutorialBucketName);return e});err!=nil{_ = db.Close();return nil,fmt.Errorf("initialize storage database: %w",err)};return &Store{db:db},nil}
func(s *Store)Close()error{if s==nil||s.db==nil{return nil};return s.db.Close()}
func(s *Store)Snapshot()(map[int64]Record,error){out:=make(map[int64]Record);err:=s.db.View(func(tx *bolt.Tx)error{return tx.Bucket(bucketName).ForEach(func(k,v []byte)error{if v==nil{return nil};id,err:=strconv.ParseInt(string(k),10,64);if err!=nil{return fmt.Errorf("invalid user key %q: %w",k,err)};var r Record;if err:=json.Unmarshal(v,&r);err!=nil{return fmt.Errorf("decode storage record for user %d: %w",id,err)};out[id]=r;return nil})});return out,err}
func(s *Store)read(tx *bolt.Tx,id int64)(Record,error){var r Record;raw:=tx.Bucket(bucketName).Get([]byte(strconv.FormatInt(id,10)));if raw==nil{return r,nil};err:=json.Unmarshal(raw,&r);return r,err}
func(s *Store)write(tx *bolt.Tx,id int64,r Record)error{raw,err:=json.Marshal(r);if err!=nil{return err};return tx.Bucket(bucketName).Put([]byte(strconv.FormatInt(id,10)),raw)}
func(s *Store)Add(id int64,d Destination)error{return s.db.Update(func(tx *bolt.Tx)error{r,err:=s.read(tx,id);if err!=nil{return err};found:=false;for i:=range r.Groups{if r.Groups[i].ChatID==d.ChatID{r.Groups[i]=d;found=true;break}};if !found{r.Groups=append(r.Groups,d)};r.ActiveChatID=d.ChatID;return s.write(tx,id,r)})}
func(s *Store)Select(id,chatID int64)error{return s.db.Update(func(tx *bolt.Tx)error{r,err:=s.read(tx,id);if err!=nil{return err};found:=false;for _,d:=range r.Groups{if d.ChatID==chatID{found=true;break}};if !found{return fmt.Errorf("storage group %d not found for user %d",chatID,id)};r.ActiveChatID=chatID;return s.write(tx,id,r)})}
func(s *Store)Forget(id,chatID int64)error{return s.db.Update(func(tx *bolt.Tx)error{r,err:=s.read(tx,id);if err!=nil{return err};kept:=make([]Destination,0,len(r.Groups));found:=false;for _,d:=range r.Groups{if d.ChatID==chatID{found=true;continue};kept=append(kept,d)};if !found{return fmt.Errorf("storage group %d not found for user %d",chatID,id)};r.Groups=kept;if r.ActiveChatID==chatID{r.ActiveChatID=0;if len(kept)>0{r.ActiveChatID=kept[len(kept)-1].ChatID}};if len(kept)==0{return tx.Bucket(bucketName).Delete([]byte(strconv.FormatInt(id,10)))};return s.write(tx,id,r)})}

func(s *Store)SetTutorialVideoID(fileID string)error{
	if strings.TrimSpace(fileID)==""{return fmt.Errorf("tutorial video file ID is empty")}
	return s.db.Update(func(tx *bolt.Tx)error{return tx.Bucket(tutorialBucketName).Put([]byte("tutorial_video_file_id"),[]byte(fileID))})
}
func(s *Store)TutorialVideoID()(string,error){
	var fileID string
	err:=s.db.View(func(tx *bolt.Tx)error{raw:=tx.Bucket(tutorialBucketName).Get([]byte("tutorial_video_file_id"));if raw!=nil{fileID=string(raw)};return nil})
	return fileID,err
}
