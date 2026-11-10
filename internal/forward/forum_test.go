package forward
import "testing"
func TestSortForumTopicsNumericallyAndKeepGeneralFirst(t *testing.T) {
 topics:=[]ForumTopic{{ID:9,Title:"10. Testing",Date:10},{ID:8,Title:"2. Backend",Date:20},{ID:7,Title:"1. Frontend",Date:30},{ID:1,Title:"General",Date:1},{ID:5,Title:"Announcements",Date:40}}
 sortForumTopics(topics);want:=[]int{1,7,8,9,5}
 for i,id:=range want { if topics[i].ID!=id { t.Fatalf("topic order IDs=%v, want %v",topics,want) } }
}
func TestTopicNumber(t *testing.T) {
 cases:=[]struct{title string;want int;ok bool}{{"1. Frontend",1,true},{"2) Backend",2,true},{"10 - Testing",10,true},{"11: DevOps",11,true},{"General",0,false},{"Frontend 1",0,false},{"1Frontend",0,false}}
 for _,tc:=range cases { got,ok:=topicNumber(tc.title);if got!=tc.want||ok!=tc.ok { t.Errorf("topicNumber(%q)=(%d,%v), want (%d,%v)",tc.title,got,ok,tc.want,tc.ok) } }
}
